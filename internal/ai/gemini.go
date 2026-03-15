package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// GeminiProvider implements AIProvider using the Google Generative AI REST API.
type GeminiProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGeminiProvider creates a new Gemini provider with default model gemini-2.0-flash.
func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		apiKey: apiKey,
		model:  "gemini-2.0-flash",
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Gemini request/response types.
type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature      float64 `json:"temperature"`
	MaxOutputTokens  int     `json:"maxOutputTokens"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata geminiUsage       `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

// Complete sends a completion request to the Gemini API with exponential backoff on 429/503.
func (g *GeminiProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	// Build Gemini request from Messages.
	var systemInstruction *geminiContent
	var contents []geminiContent

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			systemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: msg.Content}},
				Role:  "user",
			}
		case "user":
			contents = append(contents, geminiContent{
				Parts: []geminiPart{{Text: msg.Content}},
				Role:  "user",
			})
		case "assistant":
			contents = append(contents, geminiContent{
				Parts: []geminiPart{{Text: msg.Content}},
				Role:  "model",
			})
		}
	}

	genConfig := geminiGenerationConfig{
		Temperature:     req.Temperature,
		MaxOutputTokens: req.MaxTokens,
	}
	if req.Format == "json" {
		genConfig.ResponseMimeType = "application/json"
	}

	gemReq := geminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig:  genConfig,
	}

	// Use model from request if provided, else default.
	model := g.model
	if req.ModelID != "" {
		model = req.ModelID
	}

	// Retry with exponential backoff on 429/503.
	var lastErr error
	backoff := 1 * time.Second
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		resp, err := g.doRequest(ctx, model, gemReq)
		if err != nil {
			lastErr = err
			continue
		}

		latency := time.Since(start).Milliseconds()

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("gemini returned no candidates")
		}

		slog.Debug("gemini completion",
			"model", model,
			"tokens_in", resp.UsageMetadata.PromptTokenCount,
			"tokens_out", resp.UsageMetadata.CandidatesTokenCount,
			"latency_ms", latency,
		)

		return &CompletionResponse{
			Content:   resp.Candidates[0].Content.Parts[0].Text,
			TokensIn:  resp.UsageMetadata.PromptTokenCount,
			TokensOut: resp.UsageMetadata.CandidatesTokenCount,
			LatencyMs: latency,
			Model:     model,
		}, nil
	}

	return nil, fmt.Errorf("gemini request failed after 3 attempts: %w", lastErr)
}

func (g *GeminiProvider) doRequest(ctx context.Context, model string, gemReq geminiRequest) (*geminiResponse, error) {
	jsonBody, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling gemini request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", geminiBaseURL, model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading gemini response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			if secs, err := strconv.Atoi(retryAfter); err == nil {
				return nil, &retryableError{
					err:        fmt.Errorf("gemini rate limited (status %d)", resp.StatusCode),
					retryAfter: time.Duration(secs) * time.Second,
				}
			}
		}
		return nil, fmt.Errorf("gemini rate limited (status %d): %s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, fmt.Errorf("unmarshaling gemini response: %w", err)
	}

	return &gemResp, nil
}

// retryableError indicates the request should be retried.
type retryableError struct {
	err        error
	retryAfter time.Duration
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// Embed always returns an error since Gemini embeddings are not supported.
// Use Ollama nomic-embed-text instead.
func (g *GeminiProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("gemini embeddings not supported — use ollama nomic-embed-text")
}
