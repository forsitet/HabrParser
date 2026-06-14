package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"habr-tg-bot/internal/domain"
)

type Summarizer interface {
	Summarize(ctx context.Context, article domain.Article) (string, error)
}

type OpenAICompatible struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func NewOpenAICompatible(baseURL, apiKey, model string, timeout time.Duration) *OpenAICompatible {
	return &OpenAICompatible{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		http:    &http.Client{Timeout: timeout},
	}
}

func (s *OpenAICompatible) Summarize(ctx context.Context, article domain.Article) (string, error) {
	if s.baseURL == "" || s.model == "" {
		return "", fmt.Errorf("LLM_BASE_URL and LLM_MODEL are required for LLM summary")
	}

	prompt := `Ты помощник, который кратко пересказывает технические статьи.
Сделай summary на русском языке в 2-4 предложения.
Объясни:
1. О чём статья.
2. В чём её практическая польза.
3. Кому она может быть интересна.
Не добавляй факты, которых нет в статье.
Не используй рекламный стиль.
Не делай summary длиннее 500 символов.`

	content := "Заголовок: " + article.Title + "\n\nХабы: " + strings.Join(article.Hubs, ", ") + "\n\nТеги: " + strings.Join(article.Tags, ", ") + "\n\nТекст статьи:\n" + truncateRunes(article.Text, 12000)
	reqBody := chatRequest{
		Model: s.model,
		Messages: []chatMessage{
			{Role: "system", Content: prompt},
			{Role: "user", Content: content},
		},
		Temperature: 0.2,
		MaxTokens:   220,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal LLM request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create LLM request: %w", err)
	}
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call LLM: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read LLM response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM status %d: %s", resp.StatusCode, truncateRunes(string(body), 500))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode LLM response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("LLM response has no choices")
	}
	summary := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("LLM summary is empty")
	}
	return truncateRunes(summary, 500), nil
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit]))
}
