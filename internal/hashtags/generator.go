package hashtags

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"habr-tg-bot/internal/domain"
)

type Generator struct {
	max int
}

func New(max int) *Generator {
	if max <= 0 {
		max = 7
	}
	return &Generator{max: max}
}

func (g *Generator) Generate(article domain.Article, summary string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, g.max)
	add := func(value string) {
		normalized := normalize(value)
		if normalized == "" || utf8.RuneCountInString(normalized) > 32 {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		result = append(result, "#"+normalized)
	}

	for _, hub := range article.Hubs {
		add(hub)
		if len(result) >= g.max {
			return result
		}
	}
	for _, tag := range article.Tags {
		add(tag)
		if len(result) >= g.max {
			return result
		}
	}
	for _, word := range extractKeywords(article.Title + " " + summary) {
		add(word)
		if len(result) >= g.max {
			return result
		}
	}
	return result
}

func extractKeywords(value string) []string {
	words := strings.FieldsFunc(value, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '+')
	})
	counts := make(map[string]int)
	for _, word := range words {
		word = normalize(word)
		if len([]rune(word)) < 3 || isStopWord(word) {
			continue
		}
		counts[word]++
	}
	type pair struct {
		word  string
		count int
	}
	pairs := make([]pair, 0, len(counts))
	for word, count := range counts {
		pairs = append(pairs, pair{word: word, count: count})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].word < pairs[j].word
		}
		return pairs[i].count > pairs[j].count
	})
	result := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		result = append(result, pair.word)
	}
	return result
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
		if r == '_' || r == '-' || unicode.IsSpace(r) || r == '+' {
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func isStopWord(word string) bool {
	_, ok := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "this": {}, "that": {},
		"как": {}, "что": {}, "для": {}, "или": {}, "это": {}, "при": {}, "про": {}, "без": {}, "над": {}, "под": {}, "его": {}, "ее": {}, "она": {}, "они": {}, "мы": {}, "вы": {}, "их": {}, "из": {}, "на": {}, "в": {}, "во": {}, "по": {}, "за": {}, "от": {},
		"статья": {}, "summary": {}, "короткое": {}, "польза": {}, "интересна": {},
	}[word]
	return ok
}
