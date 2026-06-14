package scoring

import (
	"regexp"
	"strings"
	"unicode"

	"habr-tg-bot/internal/config"
	"habr-tg-bot/internal/domain"
)

type Scorer struct {
	cfg          config.ScoreConfig
	knownAuthors map[string]struct{}
}

type Result struct {
	Score   int
	Reasons []string
}

var (
	technicalKeywordRe  = regexp.MustCompile(`(?i)\b(api|grpc|http|tcp|udp|tls|sql|postgres|mysql|sqlite|redis|kafka|rabbitmq|docker|kubernetes|linux|go|golang|java|python|rust|typescript|javascript|c\+\+|benchmark|latency|throughput|memory|cpu|profiling|observability|monitoring|tracing|architecture|distributed|database|cache|queue|security|vulnerability|encryption|ci/cd|devops|backend|frontend|ml|llm|rag|vector|embedding)\b`)
	productionContextRe = regexp.MustCompile(`(?i)(production|prod|продакшен|прод|нагрузк|инцидент|sla|slo|on-call|deploy|deployment|релиз|миграци|отказоустойчив|масштабирован|мониторинг|логирован|трассиров|latency|throughput|rps|postmortem)`)
	advertisingRe       = regexp.MustCompile(`(?i)(подписывайтесь|промокод|скидк|регистрируйтесь|бесплатная консультация|наш продукт|наша платформа|закажите|купите|sales|lead magnet)`)
	vacancyRe           = regexp.MustCompile(`(?i)(ваканси|ищем\s+(разработчика|инженера|аналитика)|зарплата|резюме|откликнуться|hh\.ru|работа в компании)`)
	newsNoDetailsRe     = regexp.MustCompile(`(?i)(анонсировала|анонсировал|представила|представил|выпустила|выпустил|стало известно|по данным|сообщает|новость)`)
	translationRe       = regexp.MustCompile(`(?i)(перевод статьи|translation|translated by|оригинал статьи|перевод опубликован)`)
	marketingRe         = regexp.MustCompile(`(?i)(маркетинг|digital|воронка продаж|лидогенерац|бренд|позиционирован|продвижен|целевая аудитория|конверсия)`)
	genericRe           = regexp.MustCompile(`(?i)(в современном мире|ни для кого не секрет|с каждым днем|важно понимать|в этой статье мы рассмотрим|является важным аспектом)`)
)

func New(cfg config.ScoreConfig) *Scorer {
	known := make(map[string]struct{})
	for _, author := range cfg.KnownAuthors {
		author = strings.ToLower(strings.TrimSpace(author))
		if author != "" {
			known[author] = struct{}{}
		}
	}
	return &Scorer{cfg: cfg, knownAuthors: known}
}

func (s *Scorer) Score(article domain.Article, selectedCategories []string) Result {
	text := article.Title + "\n" + strings.Join(article.Hubs, " ") + "\n" + strings.Join(article.Tags, " ") + "\n" + article.Text
	score := 0
	reasons := make([]string, 0)

	if hasCategoryMatch(article, selectedCategories) {
		score += s.cfg.CategoryMatch
		reasons = append(reasons, "category_match")
	}
	if article.Rating >= s.cfg.HighRatingThreshold {
		score += s.cfg.HighRating
		reasons = append(reasons, "high_rating")
	} else if article.Rating >= s.cfg.HighRatingThreshold/2 && s.cfg.HighRatingThreshold > 1 {
		score += s.cfg.HighRating / 2
		reasons = append(reasons, "medium_rating")
	}
	if article.CommentsCount >= s.cfg.ManyCommentsThreshold {
		score += s.cfg.ManyComments
		reasons = append(reasons, "many_comments")
	} else if article.CommentsCount >= s.cfg.ManyCommentsThreshold/2 && s.cfg.ManyCommentsThreshold > 1 {
		score += s.cfg.ManyComments / 2
		reasons = append(reasons, "some_comments")
	}
	if article.AuthorKarma >= s.cfg.HighAuthorKarmaLimit {
		score += s.cfg.HighAuthorKarma
		reasons = append(reasons, "high_author_karma")
	} else if article.AuthorKarma >= s.cfg.HighAuthorKarmaLimit/2 && s.cfg.HighAuthorKarmaLimit > 1 {
		score += s.cfg.HighAuthorKarma / 2
		reasons = append(reasons, "medium_author_karma")
	}
	if _, ok := s.knownAuthors[strings.ToLower(article.AuthorName)]; ok {
		score += s.cfg.KnownAuthor
		reasons = append(reasons, "known_author")
	}
	if article.HasCodeBlocks {
		score += s.cfg.HasCode
		reasons = append(reasons, "has_code")
	}
	if technicalKeywordRe.MatchString(text) {
		score += s.cfg.TechnicalKeywords
		reasons = append(reasons, "technical_keywords")
	}
	if productionContextRe.MatchString(text) {
		score += s.cfg.ProductionContext
		reasons = append(reasons, "production_context")
	}

	words := countWords(article.Text)
	if words > 0 && words < s.cfg.TooShortWordsThreshold {
		score += s.cfg.TooShortPenalty
		reasons = append(reasons, "too_short")
	}
	if advertisingRe.MatchString(text) {
		score += s.cfg.AdvertisingPenalty
		reasons = append(reasons, "advertising")
	}
	if vacancyRe.MatchString(text) {
		score += s.cfg.VacancyPenalty
		reasons = append(reasons, "vacancy")
	}
	if newsNoDetailsRe.MatchString(text) && !article.HasCodeBlocks && !productionContextRe.MatchString(text) {
		score += s.cfg.NewsNoDetailsPenalty
		reasons = append(reasons, "news_without_details")
	}
	if translationRe.MatchString(text) && !productionContextRe.MatchString(text) {
		score += s.cfg.TranslationPenalty
		reasons = append(reasons, "plain_translation")
	}
	if marketingRe.MatchString(text) {
		score += s.cfg.MarketingPenalty
		reasons = append(reasons, "marketing")
	}
	if genericRe.MatchString(text) && !article.HasCodeBlocks && !productionContextRe.MatchString(text) {
		score += s.cfg.TooGenericPenalty
		reasons = append(reasons, "too_generic")
	}

	return Result{Score: score, Reasons: reasons}
}

func hasCategoryMatch(article domain.Article, selectedCategories []string) bool {
	selected := make(map[string]struct{}, len(selectedCategories))
	for _, category := range selectedCategories {
		category = normalize(category)
		if category != "" {
			selected[category] = struct{}{}
		}
	}
	for _, category := range append(article.Hubs, article.Tags...) {
		if _, ok := selected[normalize(category)]; ok {
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

func countWords(value string) int {
	count := 0
	inWord := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !inWord {
				count++
				inWord = true
			}
			continue
		}
		inWord = false
	}
	return count
}
