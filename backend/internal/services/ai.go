package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/fethiaksit/social-analytics/internal/domain"
	"net/http"
	"regexp"
	"strings"
)

type AIService struct {
	key, model, embeddingModel string
	client                     *http.Client
}

func NewAIService(key, model, embeddingModel string) *AIService {
	return &AIService{key: key, model: model, embeddingModel: embeddingModel, client: &http.Client{Timeout: 30_000_000_000}}
}

type analysisOutput struct {
	Summary    string   `json:"summary"`
	MainTopic  string   `json:"mainTopic"`
	SubTopic   string   `json:"subTopic"`
	Keywords   []string `json:"keywords"`
	Sentiment  string   `json:"sentiment"`
	Confidence float64  `json:"confidence"`
}

// Analyze returns a structured result. Offline mode keeps local development usable without sending content externally.
func (a *AIService) Analyze(ctx context.Context, content string) (*domain.AIAnalysis, error) {
	if a.key == "" {
		words := keywords(content)
		return &domain.AIAnalysis{Summary: truncate(content, 240), MainTopic: first(words, "Genel"), SubTopic: "Genel", Keywords: words, Sentiment: "neutral", Confidence: .5, Embedding: a.EmbedLocal(content)}, nil
	}
	prompt := "Türkçe sosyal medya metnini analiz et. Sadece JSON dön: {summary,mainTopic,subTopic,keywords,sentiment,confidence}. confidence 0-1. Metin: " + content
	body := map[string]any{"model": a.model, "input": prompt, "text": map[string]any{"format": map[string]string{"type": "json_object"}}}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/responses", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai analysis: %s", resp.Status)
	}
	var payload struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Output) == 0 || len(payload.Output[0].Content) == 0 {
		return nil, fmt.Errorf("openai returned no content")
	}
	var out analysisOutput
	if err = json.Unmarshal([]byte(payload.Output[0].Content[0].Text), &out); err != nil {
		return nil, err
	}
	embedding, err := a.Embed(ctx, content)
	if err != nil {
		return nil, err
	}
	return &domain.AIAnalysis{Summary: out.Summary, MainTopic: out.MainTopic, SubTopic: out.SubTopic, Keywords: out.Keywords, Sentiment: out.Sentiment, Confidence: out.Confidence, Embedding: embedding}, nil
}

// Embed requests a semantic vector from OpenAI; the deterministic fallback is only for local development.
func (a *AIService) Embed(ctx context.Context, text string) ([]float32, error) {
	if a.key == "" {
		return a.EmbedLocal(text), nil
	}
	raw, err := json.Marshal(map[string]string{"model": a.embeddingModel, "input": text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai embedding: %s", resp.Status)
	}
	var payload struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 {
		return nil, fmt.Errorf("openai returned no embedding")
	}
	return payload.Data[0].Embedding, nil
}

// EmbedLocal is a stable fallback vector. Replace with OpenAI embeddings in privacy-approved production deployments.
func (a *AIService) EmbedLocal(text string) []float32 {
	sum := sha256.Sum256([]byte(strings.ToLower(text)))
	v := make([]float32, len(sum))
	for i, b := range sum {
		v[i] = (float32(b) / 127.5) - 1
	}
	return v
}

var tokenRE = regexp.MustCompile(`[\pL\pN]{3,}`)

func keywords(s string) []string {
	all := tokenRE.FindAllString(strings.ToLower(s), -1)
	seen := map[string]bool{}
	out := []string{}
	for _, w := range all {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
		if len(out) == 5 {
			break
		}
	}
	return out
}
func first(a []string, d string) string {
	if len(a) > 0 {
		return a[0]
	}
	return d
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
