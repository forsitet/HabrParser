package bot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"habr-tg-bot/internal/domain"
	"habr-tg-bot/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const categoriesPageSize = 8

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
		return
	}
	if update.Message == nil || !update.Message.IsCommand() {
		return
	}

	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	if err := b.store.EnsureUser(ctx, userID); err != nil {
		b.logger.Error("failed to ensure Telegram user", "telegram_user_id", userID, "error", err)
		b.sendText(chatID, "Не удалось создать настройки пользователя. Попробуйте позже.")
		return
	}

	switch update.Message.Command() {
	case "start":
		b.handleStart(chatID)
	case "help":
		b.handleHelp(chatID)
	case "categories":
		b.handleCategories(ctx, chatID, userID, 0)
	case "my_categories":
		b.handleMyCategories(ctx, chatID, userID)
	case "latest":
		b.handleLatest(ctx, chatID, userID)
	case "today":
		b.handleToday(ctx, chatID, userID)
	case "auto_on":
		b.handleAuto(ctx, chatID, userID, true)
	case "auto_off":
		b.handleAuto(ctx, chatID, userID, false)
	default:
		b.sendText(chatID, "Неизвестная команда. Используйте /help.")
	}
}

func (b *Bot) handleStart(chatID int64) {
	b.sendText(chatID, "Привет. Я каждый день нахожу интересные статьи Хабра по выбранным хабам.\n\n/latest покажет статьи за один календарный день, который был 7 дней назад. Это не скользящее окно: если сегодня 13 июня, я беру 6 июня с 00:00:00 до 23:59:59.\n\nНачните с /categories, затем используйте /latest или включите /auto_on.")
}

func (b *Bot) handleHelp(chatID int64) {
	b.sendText(chatID, "/start — приветствие\n/categories — выбрать категории Хабра\n/my_categories — показать выбранные категории\n/latest — лучшие статьи за календарный день 7 дней назад\n/today — интересные статьи за текущий день\n/auto_on — включить ежедневную рассылку\n/auto_off — выключить ежедневную рассылку\n/help — помощь")
}

func (b *Bot) handleCategories(ctx context.Context, chatID int64, userID int64, page int) {
	text, keyboard, err := b.renderCategories(ctx, userID, page)
	if err != nil {
		b.logger.Warn("failed to render categories", "telegram_user_id", userID, "error", err)
		b.sendText(chatID, "Не удалось получить список категорий Хабра. Попробуйте позже.")
		return
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Warn("failed to send categories", "telegram_user_id", userID, "error", err)
	}
}

func (b *Bot) handleMyCategories(ctx context.Context, chatID int64, userID int64) {
	settings, err := b.store.GetUserSettings(ctx, userID)
	if err != nil {
		b.logger.Warn("failed to get user settings", "telegram_user_id", userID, "error", err)
		b.sendText(chatID, "Не удалось получить настройки.")
		return
	}
	if len(settings.Categories) == 0 {
		b.sendText(chatID, "Категории не выбраны. Откройте /categories.")
		return
	}
	titles := b.categoryTitles(ctx, settings.Categories)
	b.sendText(chatID, "Выбранные категории:\n"+strings.Join(titles, "\n"))
}

func (b *Bot) handleLatest(ctx context.Context, chatID int64, userID int64) {
	b.sendText(chatID, "Ищу интересные статьи за календарный день, который был 7 дней назад.")
	result, err := b.articles.BuildLatestDigest(ctx, userID, time.Now())
	b.sendDigestResult(ctx, chatID, userID, result, err)
}

func (b *Bot) handleToday(ctx context.Context, chatID int64, userID int64) {
	b.sendText(chatID, "Ищу интересные статьи за текущий календарный день.")
	result, err := b.articles.BuildTodayDigest(ctx, userID, time.Now())
	b.sendDigestResult(ctx, chatID, userID, result, err)
}

func (b *Bot) sendDigestResult(ctx context.Context, chatID int64, userID int64, result domain.DigestResult, err error) {
	if errors.Is(err, service.ErrNoCategories) {
		b.sendText(chatID, "Сначала выберите категории через /categories.")
		return
	}
	if err != nil {
		b.logger.Warn("failed to build digest", "telegram_user_id", userID, "error", err)
		b.sendText(chatID, "Не удалось собрать дайджест. Попробуйте позже.")
		return
	}
	if len(result.Messages) == 0 {
		b.sendText(chatID, "Подходящих статей не нашлось. Можно расширить список категорий или снизить MIN_ARTICLE_SCORE.")
		return
	}
	sent := b.SendDigestMessages(ctx, chatID, result.Messages)
	b.logger.Info("manual digest sent", "telegram_user_id", userID, "target_date", result.Stats.TargetDate.Format("2006-01-02"), "articles_found", result.Stats.ArticlesFound, "articles_passed", result.Stats.ArticlesPassed, "articles_sent", sent)
}

func (b *Bot) handleAuto(ctx context.Context, chatID int64, userID int64, enabled bool) {
	if enabled {
		settings, err := b.store.GetUserSettings(ctx, userID)
		if err != nil {
			b.logger.Warn("failed to get settings before auto_on", "telegram_user_id", userID, "error", err)
			b.sendText(chatID, "Не удалось получить настройки.")
			return
		}
		if len(settings.Categories) == 0 {
			b.sendText(chatID, "Сначала выберите категории через /categories. Авторассылка без категорий не включается.")
			return
		}
	}
	if err := b.store.SetAutoSend(ctx, userID, enabled); err != nil {
		b.logger.Warn("failed to set auto send", "telegram_user_id", userID, "enabled", enabled, "error", err)
		b.sendText(chatID, "Не удалось изменить настройку авторассылки.")
		return
	}
	if enabled {
		b.sendText(chatID, "Ежедневная авторассылка включена.")
	} else {
		b.sendText(chatID, "Ежедневная авторассылка выключена.")
	}
}

func (b *Bot) handleCallback(ctx context.Context, query *tgbotapi.CallbackQuery) {
	if query.From == nil || query.Message == nil {
		return
	}
	userID := query.From.ID
	chatID := query.Message.Chat.ID
	messageID := query.Message.MessageID

	if err := b.store.EnsureUser(ctx, userID); err != nil {
		b.answerCallback(query.ID, "Не удалось создать настройки")
		return
	}

	data := query.Data
	page := 0
	if strings.HasPrefix(data, "cat_page:") {
		page, _ = strconv.Atoi(strings.TrimPrefix(data, "cat_page:"))
	} else if strings.HasPrefix(data, "cat_toggle:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			b.answerCallback(query.ID, "Некорректная кнопка")
			return
		}
		category := parts[1]
		page, _ = strconv.Atoi(parts[2])
		selected, err := b.store.ToggleUserCategory(ctx, userID, category)
		if err != nil {
			b.logger.Warn("failed to toggle category", "telegram_user_id", userID, "category", category, "error", err)
			b.answerCallback(query.ID, "Не удалось сохранить")
			return
		}
		if selected {
			b.answerCallback(query.ID, "Категория добавлена")
		} else {
			b.answerCallback(query.ID, "Категория убрана")
		}
	} else {
		b.answerCallback(query.ID, "Неизвестное действие")
		return
	}

	text, keyboard, err := b.renderCategories(ctx, userID, page)
	if err != nil {
		b.logger.Warn("failed to refresh categories", "telegram_user_id", userID, "error", err)
		return
	}
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, keyboard)
	if _, err := b.api.Request(edit); err != nil {
		b.logger.Warn("failed to edit categories message", "telegram_user_id", userID, "error", err)
	}
}

func (b *Bot) renderCategories(ctx context.Context, userID int64, page int) (string, tgbotapi.InlineKeyboardMarkup, error) {
	categories, err := b.categories.FetchCategories(ctx)
	if err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}
	if len(categories) == 0 {
		return "", tgbotapi.InlineKeyboardMarkup{}, fmt.Errorf("empty categories list")
	}
	selected, err := b.store.GetUserCategories(ctx, userID)
	if err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, category := range selected {
		selectedSet[category] = struct{}{}
	}

	totalPages := (len(categories) + categoriesPageSize - 1) / categoriesPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	start := page * categoriesPageSize
	end := start + categoriesPageSize
	if end > len(categories) {
		end = len(categories)
	}

	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	for _, category := range categories[start:end] {
		prefix := ""
		if _, ok := selectedSet[category.Alias]; ok {
			prefix = "✓ "
		}
		label := prefix + category.Title
		if len([]rune(label)) > 45 {
			label = string([]rune(label)[:42]) + "..."
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cat_toggle:%s:%d", category.Alias, page))))
	}

	nav := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	if page > 0 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("← Назад", fmt.Sprintf("cat_page:%d", page-1)))
	}
	if page+1 < totalPages {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("Вперёд →", fmt.Sprintf("cat_page:%d", page+1)))
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}

	selectedText := "нет"
	if len(selected) > 0 {
		selectedText = strings.Join(b.categoryTitlesFromList(categories, selected), ", ")
	}
	text := fmt.Sprintf("Категории Хабра, страница %d/%d. Нажмите на категорию, чтобы выбрать или убрать её.\n\nВыбрано: %s", page+1, totalPages, selectedText)
	return text, tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

func (b *Bot) answerCallback(callbackID string, text string) {
	callback := tgbotapi.NewCallback(callbackID, text)
	if _, err := b.api.Request(callback); err != nil {
		b.logger.Warn("failed to answer callback", "error", err)
	}
}

func (b *Bot) categoryTitles(ctx context.Context, aliases []string) []string {
	categories, err := b.categories.FetchCategories(ctx)
	if err != nil {
		result := append([]string(nil), aliases...)
		sort.Strings(result)
		return result
	}
	return b.categoryTitlesFromList(categories, aliases)
}

func (b *Bot) categoryTitlesFromList(categories []domain.Category, aliases []string) []string {
	lookup := make(map[string]string, len(categories))
	for _, category := range categories {
		lookup[category.Alias] = category.Title
	}
	result := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if title, ok := lookup[alias]; ok {
			result = append(result, title)
		} else {
			result = append(result, alias)
		}
	}
	sort.Strings(result)
	return result
}
