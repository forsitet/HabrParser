package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"habr-tg-bot/internal/domain"
	"habr-tg-bot/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Store interface {
	EnsureUser(ctx context.Context, telegramUserID int64) error
	GetUserSettings(ctx context.Context, telegramUserID int64) (domain.UserSettings, error)
	GetUserCategories(ctx context.Context, telegramUserID int64) ([]string, error)
	SetUserCategories(ctx context.Context, telegramUserID int64, categories []string) error
	ToggleUserCategory(ctx context.Context, telegramUserID int64, category string) (bool, error)
	SetAutoSend(ctx context.Context, telegramUserID int64, enabled bool) error
}

type CategorySource interface {
	FetchCategories(ctx context.Context) ([]domain.Category, error)
}

type Bot struct {
	api          *tgbotapi.BotAPI
	store        Store
	categories   CategorySource
	articles     *service.ArticleService
	logger       *slog.Logger
	updatesLimit int
}

func New(token string, proxyURL string, store Store, categories CategorySource, articles *service.ArticleService, logger *slog.Logger) (*Bot, error) {
	api, err := newTelegramAPI(token, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create Telegram bot: %w", err)
	}
	b := &Bot{
		api:          api,
		store:        store,
		categories:   categories,
		articles:     articles,
		logger:       logger,
		updatesLimit: 60,
	}
	return b, nil
}

func newTelegramAPI(token string, proxyURL string) (*tgbotapi.BotAPI, error) {
	if proxyURL == "" {
		return tgbotapi.NewBotAPI(token)
	}
	parsedProxyURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse TELEGRAM_PROXY_URL: %w", err)
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(parsedProxyURL)},
		Timeout:   30 * time.Second,
	}
	return tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, client)
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.setCommands(); err != nil {
		b.logger.Warn("failed to set Telegram commands", "error", err)
	}

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = b.updatesLimit
	updates := b.api.GetUpdatesChan(updateConfig)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return nil
		case update := <-updates:
			b.handleUpdate(ctx, update)
		}
	}
}

func (b *Bot) SendDigestMessages(ctx context.Context, telegramUserID int64, messages []domain.DigestMessage) int {
	sent := 0
	for _, digestMessage := range messages {
		msg := tgbotapi.NewMessage(telegramUserID, digestMessage.HTML)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = false
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Warn("failed to send Telegram digest message", "telegram_user_id", telegramUserID, "article_url", digestMessage.Article.URL, "error", err)
			continue
		}
		sent++

		select {
		case <-ctx.Done():
			return sent
		case <-time.After(350 * time.Millisecond):
		}
	}
	return sent
}

func (b *Bot) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Warn("failed to send Telegram message", "chat_id", chatID, "error", err)
	}
}

func (b *Bot) setCommands() error {
	commands := tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{Command: "start", Description: "приветствие и настройка"},
		tgbotapi.BotCommand{Command: "categories", Description: "выбрать категории Хабра"},
		tgbotapi.BotCommand{Command: "set_categories", Description: "быстро выбрать категории списком"},
		tgbotapi.BotCommand{Command: "my_categories", Description: "мои категории"},
		tgbotapi.BotCommand{Command: "latest", Description: "лучшие статьи за день 7 дней назад"},
		tgbotapi.BotCommand{Command: "today", Description: "интересные статьи за сегодня"},
		tgbotapi.BotCommand{Command: "auto_on", Description: "включить ежедневную рассылку"},
		tgbotapi.BotCommand{Command: "auto_off", Description: "выключить ежедневную рассылку"},
		tgbotapi.BotCommand{Command: "help", Description: "помощь"},
	)
	_, err := b.api.Request(commands)
	return err
}
