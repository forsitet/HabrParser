package habr

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"habr-tg-bot/internal/domain"

	"github.com/PuerkitoBio/goquery"
)

type articleCandidate struct {
	URL         string
	PublishedAt time.Time
}

var (
	articleURLRe       = regexp.MustCompile(`href="([^"]*/(?:companies/[^/]+/)?articles/\d+/)[^"]*"`)
	datetimeRe         = regexp.MustCompile(`<time[^>]+datetime="([^"]+)"`)
	hubsPagesCountRe   = regexp.MustCompile(`"pagesCount"\s*:\s*\{\s*"HUBS"\s*:\s*(\d+)`)
	hubRefRe           = regexp.MustCompile(`"[A-Za-z0-9_\-]+"\s*:\s*\{[^{}]*?"alias"\s*:\s*"([^"]+)"[^{}]*?"titleHtml"\s*:\s*"([^"]+)"`)
	articleIDInURLRe   = regexp.MustCompile(`/(?:companies/[^/]+/)?articles/(\d+)/`)
	hubAliasInURLRe    = regexp.MustCompile(`/hubs/([^/]+)/`)
	integerRe          = regexp.MustCompile(`[+\-−]?\d+`)
	floatRe            = regexp.MustCompile(`[+\-−]?\d+(?:[\.,]\d+)?`)
	spaceRe            = regexp.MustCompile(`\s+`)
	jsonKarmaRe        = regexp.MustCompile(`(?i)"karma"\s*:\s*([+\-]?\d+(?:\.\d+)?)`)
	visibleKarmaTextRe = regexp.MustCompile(`(?i)(?:карма|karma)[^+\-−\d]{0,80}([+\-−]?\d+(?:[\.,]\d+)?)`)
	stripTagsRe        = regexp.MustCompile(`<[^>]+>`)
)

func parseArticleList(body, baseURL string) []articleCandidate {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return parseArticleListFallback(body, baseURL)
	}

	candidates := make([]articleCandidate, 0)
	seen := make(map[string]struct{})
	doc.Find("article").Each(func(_ int, article *goquery.Selection) {
		link := article.Find("a.tm-title__link").First()
		if link.Length() == 0 {
			link = article.Find("a[href*='/articles/']").First()
		}
		href, ok := link.Attr("href")
		if !ok || href == "" {
			return
		}
		articleURL := absoluteURL(baseURL, href)
		articleURL = normalizeArticleURL(articleURL)
		if articleURL == "" {
			return
		}
		if _, ok := seen[articleURL]; ok {
			return
		}
		seen[articleURL] = struct{}{}

		published := time.Time{}
		if datetime, ok := article.Find("time").First().Attr("datetime"); ok {
			published = parseTime(datetime)
		}
		candidates = append(candidates, articleCandidate{URL: articleURL, PublishedAt: published})
	})

	if len(candidates) == 0 {
		return parseArticleListFallback(body, baseURL)
	}
	return candidates
}

func parseArticleListFallback(body, baseURL string) []articleCandidate {
	urlMatches := articleURLRe.FindAllStringSubmatch(body, -1)
	timeMatches := datetimeRe.FindAllStringSubmatch(body, -1)
	candidates := make([]articleCandidate, 0, len(urlMatches))
	seen := make(map[string]struct{})
	for i, match := range urlMatches {
		articleURL := normalizeArticleURL(absoluteURL(baseURL, html.UnescapeString(match[1])))
		if articleURL == "" {
			continue
		}
		if _, ok := seen[articleURL]; ok {
			continue
		}
		seen[articleURL] = struct{}{}
		published := time.Time{}
		if i < len(timeMatches) {
			published = parseTime(timeMatches[i][1])
		}
		candidates = append(candidates, articleCandidate{URL: articleURL, PublishedAt: published})
	}
	return candidates
}

func parseArticlePage(body, articleURL string) (domain.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return domain.Article{}, fmt.Errorf("parse article html: %w", err)
	}

	title := cleanText(doc.Find("h1.tm-title").First().Text())
	if title == "" {
		title = cleanText(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = attrContent(doc, `meta[property="og:title"]`)
	}
	if title == "" {
		return domain.Article{}, fmt.Errorf("article title not found")
	}

	published := time.Time{}
	if datetime, ok := doc.Find("time").First().Attr("datetime"); ok {
		published = parseTime(datetime)
	}
	if published.IsZero() {
		published = parseTime(attrContent(doc, `meta[property="article:published_time"]`))
	}
	if published.IsZero() {
		return domain.Article{}, fmt.Errorf("article published time not found")
	}

	content := doc.Find(".article-formatted-body").First()
	if content.Length() == 0 {
		content = doc.Find(".tm-article-body").First()
	}
	if content.Length() == 0 {
		content = doc.Find("article").First()
	}
	content.Find("script, style, noscript, svg").Remove()
	text := cleanText(content.Text())

	article := domain.Article{
		Title:         title,
		URL:           normalizeArticleURL(articleURL),
		PublishedAt:   published,
		Rating:        parseFirstInt(doc.Find(".tm-votes-meter__value, [class*='votes-meter__value'], [class*='score']").Text()),
		CommentsCount: parseFirstInt(doc.Find(".tm-article-comments-counter-link__value, a[href$='#comments'], [class*='comments']").Text()),
		AuthorName:    cleanText(doc.Find("a.tm-user-info__username, a[href*='/users/']").First().Text()),
		AuthorURL:     "",
		AuthorKarma:   parseAuthorKarma(body),
		Hubs:          parseArticleHubs(doc),
		Tags:          parseArticleTags(doc),
		Text:          text,
		HasCodeBlocks: content.Find("pre").Length() > 0,
	}
	if href, ok := doc.Find("a.tm-user-info__username, a[href*='/users/']").First().Attr("href"); ok {
		article.AuthorURL = absoluteURL(baseFromURL(article.URL), href)
	}
	if article.URL == "" {
		article.URL = normalizeArticleURL(attrContent(doc, `link[rel="canonical"]`))
	}
	return article, nil
}

func parseCategories(body string) []domain.Category {
	categories := make([]domain.Category, 0)
	seen := make(map[string]struct{})
	for _, match := range hubRefRe.FindAllStringSubmatch(body, -1) {
		alias := decodeJSONString(match[1])
		title := cleanTitleHTML(decodeJSONString(match[2]))
		if alias == "" || title == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		categories = append(categories, domain.Category{Alias: alias, Title: title})
	}
	if len(categories) > 0 {
		return categories
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return categories
	}
	doc.Find("a[href*='/hubs/']").Each(func(_ int, link *goquery.Selection) {
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		alias := extractHubAlias(href)
		title := cleanText(link.Text())
		if alias == "" || title == "" {
			return
		}
		if _, ok := seen[alias]; ok {
			return
		}
		seen[alias] = struct{}{}
		categories = append(categories, domain.Category{Alias: alias, Title: title})
	})
	return categories
}

func parseHubsPagesCount(body string) int {
	match := hubsPagesCountRe.FindStringSubmatch(body)
	if len(match) != 2 {
		return 1
	}
	value, err := strconv.Atoi(match[1])
	if err != nil || value < 1 {
		return 1
	}
	return value
}

func parseArticleHubs(doc *goquery.Document) []string {
	seen := make(map[string]struct{})
	hubs := make([]string, 0)
	doc.Find("a[href*='/hubs/']").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		alias := extractHubAlias(href)
		if alias == "" {
			alias = normalizeCategory(cleanText(link.Text()))
		}
		if alias == "" {
			return
		}
		if _, ok := seen[alias]; ok {
			return
		}
		seen[alias] = struct{}{}
		hubs = append(hubs, alias)
	})
	return hubs
}

func parseArticleTags(doc *goquery.Document) []string {
	seen := make(map[string]struct{})
	tags := make([]string, 0)
	doc.Find("a.tm-tags-list__link, a[href*='/search/']").Each(func(_ int, link *goquery.Selection) {
		tag := normalizeCategory(cleanText(link.Text()))
		if tag == "" {
			return
		}
		if _, ok := seen[tag]; ok {
			return
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	})
	return tags
}

func parseAuthorKarma(body string) float64 {
	for _, re := range []*regexp.Regexp{jsonKarmaRe, visibleKarmaTextRe} {
		match := re.FindStringSubmatch(body)
		if len(match) == 2 {
			value := strings.ReplaceAll(strings.ReplaceAll(match[1], "−", "-"), ",", ".")
			parsed, err := strconv.ParseFloat(value, 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func absoluteURL(baseURL, href string) string {
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(html.UnescapeString(href))
	if err != nil {
		return ""
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return base.ResolveReference(parsed).String()
}

func baseFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "https://habr.com"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func extractHubAlias(href string) string {
	match := hubAliasInURLRe.FindStringSubmatch(href)
	if len(match) != 2 {
		return ""
	}
	return normalizeCategory(match[1])
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	formats := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-07:00", time.RFC1123Z, time.RFC1123}
	for _, format := range formats {
		parsed, err := time.Parse(format, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func parseFirstInt(value string) int {
	match := integerRe.FindString(value)
	if match == "" {
		return 0
	}
	match = strings.ReplaceAll(match, "−", "-")
	parsed, err := strconv.Atoi(match)
	if err != nil {
		return 0
	}
	return parsed
}

func attrContent(doc *goquery.Document, selector string) string {
	value, _ := doc.Find(selector).First().Attr("content")
	if value == "" {
		value, _ = doc.Find(selector).First().Attr("href")
	}
	return cleanText(value)
}

func cleanText(value string) string {
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.TrimSpace(spaceRe.ReplaceAllString(value, " "))
	return value
}

func cleanTitleHTML(value string) string {
	return cleanText(stripTagsRe.ReplaceAllString(value, ""))
}

func decodeJSONString(value string) string {
	decoded, err := strconv.Unquote(`"` + value + `"`)
	if err != nil {
		return value
	}
	return decoded
}

func normalizeCategory(value string) string {
	value = strings.ToLower(cleanText(value))
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
