package habr

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"habr-tg-bot/internal/domain"
)

const (
	userAgent        = "habr-tg-bot/1.0 (+https://github.com/local/habr-tg-bot)"
	maxArticlePages  = 80
	maxCategoryPages = 30
)

type Client struct {
	baseURL string
	http    *http.Client
	logger  *slog.Logger
}

func NewClient(baseURL string, timeout time.Duration, logger *slog.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		logger:  logger,
	}
}

func (c *Client) FetchArticlesByDate(ctx context.Context, targetDate time.Time, loc *time.Location) ([]domain.Article, error) {
	start := time.Date(targetDate.In(loc).Year(), targetDate.In(loc).Month(), targetDate.In(loc).Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	locales := []string{"ru", "en"}
	seen := make(map[string]struct{})
	articles := make([]domain.Article, 0)
	var lastErr error

	for _, locale := range locales {
		for page := 1; page <= maxArticlePages; page++ {
			listURL := c.articlesPageURL(locale, page)
			body, err := c.fetch(ctx, listURL)
			if err != nil {
				lastErr = err
				c.logger.Warn("failed to fetch Habr article list page", "url", listURL, "error", err)
				break
			}

			candidates := parseArticleList(body, c.baseURL)
			if len(candidates) == 0 {
				break
			}

			pageHasOlder := false
			pageHasRelevantOrNewer := false
			for _, candidate := range candidates {
				normalizedURL := normalizeArticleURL(candidate.URL)
				if normalizedURL == "" {
					continue
				}
				if _, ok := seen[normalizedURL]; ok {
					continue
				}

				if !candidate.PublishedAt.IsZero() {
					published := candidate.PublishedAt.In(loc)
					if published.Before(start) {
						pageHasOlder = true
						continue
					}
					if !published.Before(end) {
						pageHasRelevantOrNewer = true
						continue
					}
					pageHasRelevantOrNewer = true
				}

				article, err := c.FetchArticle(ctx, normalizedURL)
				if err != nil {
					lastErr = err
					c.logger.Warn("failed to fetch Habr article", "url", normalizedURL, "error", err)
					seen[normalizedURL] = struct{}{}
					continue
				}
				seen[normalizedURL] = struct{}{}

				published := article.PublishedAt.In(loc)
				if published.Before(start) {
					pageHasOlder = true
					continue
				}
				if !published.Before(end) {
					pageHasRelevantOrNewer = true
					continue
				}
				pageHasRelevantOrNewer = true
				articles = append(articles, article)
			}

			if pageHasOlder && !pageHasRelevantOrNewer {
				break
			}
		}
	}

	sort.SliceStable(articles, func(i, j int) bool {
		return articles[i].PublishedAt.After(articles[j].PublishedAt)
	})
	if len(articles) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return articles, nil
}

func (c *Client) FetchArticle(ctx context.Context, articleURL string) (domain.Article, error) {
	body, err := c.fetch(ctx, articleURL)
	if err != nil {
		return domain.Article{}, err
	}
	article, err := parseArticlePage(body, articleURL)
	if err != nil {
		return domain.Article{}, err
	}
	if article.AuthorURL != "" {
		profileBody, err := c.fetch(ctx, article.AuthorURL)
		if err != nil {
			c.logger.Debug("failed to fetch Habr author profile", "url", article.AuthorURL, "error", err)
		} else {
			article.AuthorKarma = parseAuthorKarma(profileBody)
		}
	}
	return article, nil
}

func (c *Client) FetchCategories(ctx context.Context) ([]domain.Category, error) {
	seen := make(map[string]domain.Category)
	var lastErr error
	for _, locale := range []string{"ru", "en"} {
		pagesCount := 1
		for page := 1; page <= pagesCount && page <= maxCategoryPages; page++ {
			pageURL := c.hubsPageURL(locale, page)
			body, err := c.fetch(ctx, pageURL)
			if err != nil {
				lastErr = err
				c.logger.Warn("failed to fetch Habr hubs page", "url", pageURL, "error", err)
				break
			}
			if page == 1 {
				if parsedPages := parseHubsPagesCount(body); parsedPages > pagesCount {
					pagesCount = parsedPages
				}
			}
			for _, category := range parseCategories(body) {
				if category.Alias == "" || category.Title == "" {
					continue
				}
				if _, ok := seen[category.Alias]; !ok {
					seen[category.Alias] = category
				}
			}
		}
	}

	categories := make([]domain.Category, 0, len(seen))
	for _, category := range seen {
		categories = append(categories, category)
	}
	sort.SliceStable(categories, func(i, j int) bool {
		return strings.ToLower(categories[i].Title) < strings.ToLower(categories[j].Title)
	})
	if len(categories) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return categories, nil
}

func (c *Client) fetch(ctx context.Context, rawURL string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "ru,en;q=0.8")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
			closeErr := resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if closeErr != nil {
				lastErr = closeErr
			} else if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
				lastErr = fmt.Errorf("temporary status %d", resp.StatusCode)
			} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
			} else {
				return string(body), nil
			}
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(attempt) * 700 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("fetch %s: %w", rawURL, lastErr)
}

func (c *Client) articlesPageURL(locale string, page int) string {
	if page <= 1 {
		return fmt.Sprintf("%s/%s/articles/", c.baseURL, locale)
	}
	return fmt.Sprintf("%s/%s/articles/page%d/", c.baseURL, locale, page)
}

func (c *Client) hubsPageURL(locale string, page int) string {
	if page <= 1 {
		return fmt.Sprintf("%s/%s/hubs/", c.baseURL, locale)
	}
	return fmt.Sprintf("%s/%s/hubs/page%d/", c.baseURL, locale, page)
}

func normalizeArticleURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	result := parsed.String()
	if !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result
}
