package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// OllamaCloudProvider implements AIProvider using the Ollama Cloud REST API.
type OllamaCloudProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOllamaCloudProvider creates a new Ollama Cloud provider with optimized HTTP transport.
func NewOllamaCloudProvider(baseURL, apiKey string) *OllamaCloudProvider {
	return &OllamaCloudProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ollamaChatRequest is the Ollama /chat endpoint request body.
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message         ollamaMessage `json:"message"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
	Done            bool          `json:"done"`
}

// Complete sends a chat completion request to the Ollama Cloud API.
func (o *OllamaCloudProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	timeout := 30 * time.Second
	if t, ok := req.Options["timeout"]; ok {
		if d, ok := t.(time.Duration); ok {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msgs := make([]ollamaMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = ollamaMessage{Role: m.Role, Content: m.Content}
	}

	opts := map[string]any{
		"temperature": req.Temperature,
		"num_predict": req.MaxTokens,
	}

	body := ollamaChatRequest{
		Model:    req.ModelID,
		Messages: msgs,
		Stream:   false,
		Format:   req.Format,
		Options:  opts,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ollamaChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshaling ollama response: %w", err)
	}

	if !chatResp.Done {
		return nil, fmt.Errorf("ollama response incomplete (done=false)")
	}

	latency := time.Since(start).Milliseconds()

	slog.Debug("ollama completion",
		"model", req.ModelID,
		"tokens_in", chatResp.PromptEvalCount,
		"tokens_out", chatResp.EvalCount,
		"latency_ms", latency,
	)

	// Strip markdown code block wrappers if model returns ```json ... ```
	content := chatResp.Message.Content
	if strings.HasPrefix(strings.TrimSpace(content), "```") {
		content = strings.TrimSpace(content)
		// Remove opening ```json or ```
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
		// Remove closing ```
		if idx := strings.LastIndex(content, "```"); idx != -1 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}

	return &CompletionResponse{
		Content:   content,
		TokensIn:  chatResp.PromptEvalCount,
		TokensOut: chatResp.EvalCount,
		LatencyMs: latency,
		Model:     req.ModelID,
	}, nil
}

// ollamaEmbedRequest is the Ollama /embed endpoint request body.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed generates vector embeddings using the Ollama nomic-embed-text model.
func (o *OllamaCloudProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	body := ollamaEmbedRequest{
		Model: "nomic-embed-text",
		Input: text,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/embed", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading embed response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp ollamaEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshaling embed response: %w", err)
	}

	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed returned no embeddings")
	}

	return embedResp.Embeddings[0], nil
}
