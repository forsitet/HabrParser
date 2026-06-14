package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"habr-tg-bot/internal/domain"
	"habr-tg-bot/internal/service"
)

type Store interface {
	ListAutoSendUsers(ctx context.Context) ([]domain.UserSettings, error)
}

type Sender interface {
	SendDigestMessages(ctx context.Context, telegramUserID int64, messages []domain.DigestMessage) int
}

type Scheduler struct {
	store     Store
	articles  *service.ArticleService
	sender    Sender
	dailyTime string
	loc       *time.Location
	logger    *slog.Logger
}

func New(store Store, articles *service.ArticleService, sender Sender, dailyTime string, loc *time.Location, logger *slog.Logger) *Scheduler {
	return &Scheduler{store: store, articles: articles, sender: sender, dailyTime: dailyTime, loc: loc, logger: logger}
}

func (s *Scheduler) Run(ctx context.Context) error {
	for {
		next, err := s.nextRun(time.Now())
		if err != nil {
			return err
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-timer.C:
			s.runOnce(ctx, time.Now())
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context, now time.Time) {
	targetDate := service.TargetDate(now, s.loc)
	s.logger.Info("daily digest started", "target_date", targetDate.Format("2006-01-02"))

	users, err := s.store.ListAutoSendUsers(ctx)
	if err != nil {
		s.logger.Error("failed to list auto-send users", "error", err)
		return
	}

	processed := 0
	articlesFound := 0
	articlesPassed := 0
	articlesSent := 0
	for _, user := range users {
		processed++
		if len(user.Categories) == 0 {
			s.logger.Info("skip auto-send user without categories", "telegram_user_id", user.TelegramUserID)
			continue
		}

		result, err := s.articles.BuildDigestForDate(ctx, user.TelegramUserID, targetDate)
		if errors.Is(err, service.ErrNoCategories) {
			s.logger.Info("skip auto-send user without categories", "telegram_user_id", user.TelegramUserID)
			continue
		}
		if err != nil {
			s.logger.Warn("failed to build auto digest for user", "telegram_user_id", user.TelegramUserID, "error", err)
			continue
		}

		if result.Stats.ArticlesFound > articlesFound {
			articlesFound = result.Stats.ArticlesFound
		}
		articlesPassed += result.Stats.ArticlesPassed
		if len(result.Messages) == 0 {
			continue
		}
		articlesSent += s.sender.SendDigestMessages(ctx, user.TelegramUserID, result.Messages)
	}

	s.logger.Info("daily digest finished", "target_date", targetDate.Format("2006-01-02"), "articles_found", articlesFound, "articles_passed", articlesPassed, "articles_sent", articlesSent, "users_processed", processed)
}

func (s *Scheduler) nextRun(now time.Time) (time.Time, error) {
	parsed, err := time.Parse("15:04", s.dailyTime)
	if err != nil {
		return time.Time{}, err
	}
	localNow := now.In(s.loc)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, s.loc)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}
