package summary

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"habr-tg-bot/internal/domain"
)

var sentenceSplitRe = regexp.MustCompile(`([.!?。！？]+)\s+`)

func Fallback(article domain.Article) string {
	text := strings.TrimSpace(article.Text)
	if text == "" {
		text = article.Title
	}
	text = strings.Join(strings.Fields(text), " ")
	sentences := firstSentences(text, 3)
	if sentences == "" {
		sentences = text
	}
	return truncateFallback(sentences, 500)
}

func firstSentences(text string, max int) string {
	if max <= 0 {
		return ""
	}
	matches := sentenceSplitRe.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	end := len(text)
	if len(matches) >= max {
		end = matches[max-1][1]
	}
	return strings.TrimSpace(text[:end])
}

func truncateFallback(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	trimmed := strings.TrimSpace(string(runes[:limit-1]))
	return trimmed + "..."
}
