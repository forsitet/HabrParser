package service

import (
	"context"
	"errors"
	"html"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	"habr-tg-bot/internal/domain"
	"habr-tg-bot/internal/hashtags"
	"habr-tg-bot/internal/scoring"
	"habr-tg-bot/internal/summary"
)

var ErrNoCategories = errors.New("user has no selected categories")

type Store interface {
	GetUserSettings(ctx context.Context, telegramUserID int64) (domain.UserSettings, error)
}

type ArticleClient interface {
	FetchArticlesByDate(ctx context.Context, targetDate time.Time, loc *time.Location) ([]domain.Article, error)
}

type ArticleService struct {
	store      Store
	client     ArticleClient
	scorer     *scoring.Scorer
	summarizer summary.Summarizer
	hashtags   *hashtags.Generator
	minScore   int
	maxItems   int
	loc        *time.Location
	logger     *slog.Logger
}

func NewArticleService(store Store, client ArticleClient, scorer *scoring.Scorer, summarizer summary.Summarizer, hashtagGenerator *hashtags.Generator, minScore, maxItems int, loc *time.Location, logger *slog.Logger) *ArticleService {
	return &ArticleService{
		store:      store,
		client:     client,
		scorer:     scorer,
		summarizer: summarizer,
		hashtags:   hashtagGenerator,
		minScore:   minScore,
		maxItems:   maxItems,
		loc:        loc,
		logger:     logger,
	}
}

func (s *ArticleService) BuildLatestDigest(ctx context.Context, telegramUserID int64, now time.Time) (domain.DigestResult, error) {
	targetDate := TargetDate(now, s.loc)
	return s.BuildDigestForDate(ctx, telegramUserID, targetDate)
}

func (s *ArticleService) BuildTodayDigest(ctx context.Context, telegramUserID int64, now time.Time) (domain.DigestResult, error) {
	today := now.In(s.loc)
	targetDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, s.loc)
	return s.BuildDigestForDate(ctx, telegramUserID, targetDate)
}

func (s *ArticleService) BuildDigestForDate(ctx context.Context, telegramUserID int64, targetDate time.Time) (domain.DigestResult, error) {
	settings, err := s.store.GetUserSettings(ctx, telegramUserID)
	if err != nil {
		return domain.DigestResult{}, err
	}
	if len(settings.Categories) == 0 {
		return domain.DigestResult{}, ErrNoCategories
	}

	articles, err := s.client.FetchArticlesByDate(ctx, targetDate, s.loc)
	if err != nil {
		return domain.DigestResult{}, err
	}

	type scoredArticle struct {
		article domain.Article
		score   int
	}
	scored := make([]scoredArticle, 0)
	for _, article := range articles {
		if !matchesSelectedCategories(article, settings.Categories) {
			continue
		}
		result := s.scorer.Score(article, settings.Categories)
		if result.Score < s.minScore {
			continue
		}
		scored = append(scored, scoredArticle{article: article, score: result.Score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].article.PublishedAt.After(scored[j].article.PublishedAt)
		}
		return scored[i].score > scored[j].score
	})
	if len(scored) > s.maxItems {
		scored = scored[:s.maxItems]
	}

	messages := make([]domain.DigestMessage, 0, len(scored))
	for _, item := range scored {
		summaryText, err := s.summarizer.Summarize(ctx, item.article)
		if err != nil {
			s.logger.Warn("LLM summary failed, using fallback", "article_url", item.article.URL, "error", err)
			summaryText = summary.Fallback(item.article)
		}
		tags := s.hashtags.Generate(item.article, summaryText)
		message := domain.DigestMessage{
			Article:  item.article,
			Score:    item.score,
			Summary:  summaryText,
			Hashtags: tags,
		}
		message.HTML = FormatTelegramHTML(message)
		messages = append(messages, message)
	}

	return domain.DigestResult{
		Messages: messages,
		Stats: domain.DigestStats{
			TargetDate:     startOfDay(targetDate, s.loc),
			ArticlesFound:  len(articles),
			ArticlesPassed: len(scored),
			ArticlesSent:   len(messages),
		},
	}, nil
}

func TargetDate(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc).AddDate(0, 0, -7)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

func FormatTelegramHTML(message domain.DigestMessage) string {
	title := html.EscapeString(message.Article.Title)
	articleURL := html.EscapeString(message.Article.URL)
	summaryText := html.EscapeString(strings.TrimSpace(message.Summary))
	hashtagsText := html.EscapeString(strings.Join(message.Hashtags, " "))
	return `<a href="` + articleURL + `">` + title + `</a>` + "\n\n" +
		summaryText + "\n\n" + hashtagsText
}

func matchesSelectedCategories(article domain.Article, selectedCategories []string) bool {
	selected := make(map[string]struct{}, len(selectedCategories))
	for _, category := range selectedCategories {
		category = normalize(category)
		if category != "" {
			selected[category] = struct{}{}
		}
	}
	for _, value := range append(article.Hubs, article.Tags...) {
		if _, ok := selected[normalize(value)]; ok {
			return true
		}
	}
	return false
}

func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if r == '_' || r == '-' || unicode.IsSpace(r) {
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func startOfDay(value time.Time, loc *time.Location) time.Time {
	local := value.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}
