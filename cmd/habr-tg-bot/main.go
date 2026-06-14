package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"habr-tg-bot/internal/bot"
	"habr-tg-bot/internal/config"
	"habr-tg-bot/internal/habr"
	"habr-tg-bot/internal/hashtags"
	"habr-tg-bot/internal/logging"
	"habr-tg-bot/internal/scheduler"
	"habr-tg-bot/internal/scoring"
	"habr-tg-bot/internal/service"
	"habr-tg-bot/internal/storage"
	"habr-tg-bot/internal/summary"
)

func main() {
	cfg, err := config.Load()
	logger := logging.New("info")
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger = logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	loc, err := timeLocation(cfg.Timezone)
	if err != nil {
		logger.Error("failed to load timezone", "timezone", cfg.Timezone, "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := storage.NewSQLite(ctx, cfg.SQLitePath, logger)
	if err != nil {
		logger.Error("failed to initialize sqlite", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warn("failed to close sqlite", "error", err)
		}
	}()

	habrClient := habr.NewClient(cfg.HabrBaseURL, cfg.HTTPTimeout, logger)
	scorer := scoring.New(cfg.Score)
	summarizer := summary.NewOpenAICompatible(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.HTTPTimeout)
	hashtagGenerator := hashtags.New(7)
	articleService := service.NewArticleService(store, habrClient, scorer, summarizer, hashtagGenerator, cfg.MinArticleScore, cfg.MaxArticlesPerDigest, loc, logger)

	telegramBot, err := bot.New(cfg.TelegramBotToken, store, habrClient, articleService, logger)
	if err != nil {
		logger.Error("failed to initialize Telegram bot", "error", err)
		os.Exit(1)
	}
	digestScheduler := scheduler.New(store, articleService, telegramBot, cfg.DailyDigestTime, loc, logger)

	errCh := make(chan error, 2)
	go func() {
		errCh <- telegramBot.Run(ctx)
	}()
	go func() {
		errCh <- digestScheduler.Run(ctx)
	}()

	logger.Info("habr telegram bot started", "timezone", cfg.Timezone, "daily_digest_time", cfg.DailyDigestTime)
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			logger.Error("application component stopped with error", "error", err)
		}
		cancel()
	}
	logger.Info("habr telegram bot stopped")
}

func timeLocation(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}
