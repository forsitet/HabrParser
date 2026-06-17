package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelegramBotToken     string
	TelegramProxyURL     string
	SQLitePath           string
	HabrBaseURL          string
	HTTPTimeout          time.Duration
	DailyDigestTime      string
	Timezone             string
	LLMBaseURL           string
	LLMAPIKey            string
	LLMModel             string
	MinArticleScore      int
	MaxArticlesPerDigest int
	LogLevel             string
	Score                ScoreConfig
}

type ScoreConfig struct {
	CategoryMatch          int
	HighRating             int
	ManyComments           int
	HighAuthorKarma        int
	KnownAuthor            int
	HasCode                int
	TechnicalKeywords      int
	ProductionContext      int
	AdvertisingPenalty     int
	VacancyPenalty         int
	TooShortPenalty        int
	TooGenericPenalty      int
	NewsNoDetailsPenalty   int
	TranslationPenalty     int
	MarketingPenalty       int
	HighRatingThreshold    int
	ManyCommentsThreshold  int
	HighAuthorKarmaLimit   float64
	TooShortWordsThreshold int
	KnownAuthors           []string
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken:     strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramProxyURL:     strings.TrimSpace(os.Getenv("TELEGRAM_PROXY_URL")),
		SQLitePath:           getEnv("SQLITE_PATH", "data/habr_tg_bot.sqlite"),
		HabrBaseURL:          strings.TrimRight(getEnv("HABR_BASE_URL", "https://habr.com"), "/"),
		HTTPTimeout:          getDurationEnv("HTTP_TIMEOUT", 20*time.Second),
		DailyDigestTime:      getEnv("DAILY_DIGEST_TIME", "09:00"),
		Timezone:             getEnv("TIMEZONE", "Europe/Moscow"),
		LLMBaseURL:           strings.TrimRight(getEnv("LLM_BASE_URL", "http://localhost:11434"), "/"),
		LLMAPIKey:            strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMModel:             getEnv("LLM_MODEL", "qwen2.5:3b-instruct"),
		MinArticleScore:      getIntEnv("MIN_ARTICLE_SCORE", 60),
		MaxArticlesPerDigest: getIntEnv("MAX_ARTICLES_PER_DIGEST", 5),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		Score: ScoreConfig{
			CategoryMatch:          getIntEnv("SCORE_CATEGORY_MATCH", 30),
			HighRating:             getIntEnv("SCORE_HIGH_RATING", 20),
			ManyComments:           getIntEnv("SCORE_MANY_COMMENTS", 15),
			HighAuthorKarma:        getIntEnv("SCORE_HIGH_AUTHOR_KARMA", 15),
			KnownAuthor:            getIntEnv("SCORE_KNOWN_AUTHOR", 10),
			HasCode:                getIntEnv("SCORE_HAS_CODE", 10),
			TechnicalKeywords:      getIntEnv("SCORE_TECHNICAL_KEYWORDS", 10),
			ProductionContext:      getIntEnv("SCORE_PRODUCTION_CONTEXT", 10),
			AdvertisingPenalty:     getIntEnv("SCORE_ADVERTISING_PENALTY", -30),
			VacancyPenalty:         getIntEnv("SCORE_VACANCY_PENALTY", -20),
			TooShortPenalty:        getIntEnv("SCORE_TOO_SHORT_PENALTY", -15),
			TooGenericPenalty:      getIntEnv("SCORE_TOO_GENERIC_PENALTY", -15),
			NewsNoDetailsPenalty:   getIntEnv("SCORE_NEWS_NO_DETAILS_PENALTY", -15),
			TranslationPenalty:     getIntEnv("SCORE_TRANSLATION_PENALTY", -15),
			MarketingPenalty:       getIntEnv("SCORE_MARKETING_PENALTY", -30),
			HighRatingThreshold:    getIntEnv("SCORE_HIGH_RATING_THRESHOLD", 20),
			ManyCommentsThreshold:  getIntEnv("SCORE_MANY_COMMENTS_THRESHOLD", 20),
			HighAuthorKarmaLimit:   getFloatEnv("SCORE_HIGH_AUTHOR_KARMA_LIMIT", 100),
			TooShortWordsThreshold: getIntEnv("SCORE_TOO_SHORT_WORDS_THRESHOLD", 400),
			KnownAuthors:           splitCSV(os.Getenv("KNOWN_AUTHORS")),
		},
	}

	if cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.MaxArticlesPerDigest <= 0 {
		cfg.MaxArticlesPerDigest = 5
	}
	if _, err := time.Parse("15:04", cfg.DailyDigestTime); err != nil {
		return Config{}, fmt.Errorf("DAILY_DIGEST_TIME must have HH:MM format: %w", err)
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return Config{}, fmt.Errorf("invalid TIMEZONE %q: %w", cfg.Timezone, err)
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}

func getIntEnv(key string, def int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return parsed
}

func getFloatEnv(key string, def float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return def
	}
	return parsed
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return time.Duration(seconds) * time.Second
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
