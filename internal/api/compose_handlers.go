package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// ComposeHandlers contains HTTP handlers for AI-powered email composition.
type ComposeHandlers struct {
	ai ai.AIProvider
}

// NewComposeHandlers creates a new ComposeHandlers instance.
func NewComposeHandlers(aiProvider ai.AIProvider) *ComposeHandlers {
	return &ComposeHandlers{ai: aiProvider}
}

// HandleAiDraft handles POST /api/v1/compose/ai-draft.
func (h *ComposeHandlers) HandleAiDraft(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Context   string `json:"context"`
		Tone      string `json:"tone"`
		Formality int    `json:"formality"`
		To        string `json:"to"`
		Subject   string `json:"subject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Tone == "" {
		req.Tone = "professional"
	}
	if req.Formality == 0 {
		req.Formality = 3
	}

	// If no AI provider, return a helpful fallback
	if h.ai == nil {
		WriteJSON(w, r, http.StatusOK, map[string]string{
			"body": "AI provider not configured. Please set OLLAMA_CLOUD_API_KEY in your environment.",
		})
		return
	}

	// Build the prompt
	formalityDesc := "moderately formal"
	switch {
	case req.Formality <= 1:
		formalityDesc = "very casual and informal"
	case req.Formality == 2:
		formalityDesc = "casual but respectful"
	case req.Formality == 3:
		formalityDesc = "moderately formal"
	case req.Formality == 4:
		formalityDesc = "formal and polished"
	case req.Formality >= 5:
		formalityDesc = "very formal and dignified"
	}

	systemPrompt := fmt.Sprintf(
		`You are an AI executive assistant named Regent. Draft an email body based on the user's instructions.

Rules:
- Tone: %s
- Formality: %s
- Write ONLY the email body — no subject line, no "Subject:", no greeting like "Dear" unless it fits naturally
- Do NOT include any meta-commentary or explanations
- Keep it concise but complete
- Match the tone and formality exactly as specified
- Output plain text, no markdown formatting`,
		req.Tone, formalityDesc,
	)

	userPrompt := req.Context
	if userPrompt == "" {
		userPrompt = "Write a brief professional email."
	}
	if req.Subject != "" {
		userPrompt = fmt.Sprintf("Subject: %s\n\n%s", req.Subject, userPrompt)
	}
	if req.To != "" {
		userPrompt = fmt.Sprintf("To: %s\n%s", req.To, userPrompt)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.ai.Complete(ctx, ai.CompletionRequest{
		ModelID: "gemma3:12b",
		Messages: []ai.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.7,
		MaxTokens:   500,
	})
	if err != nil {
		slog.Error("AI draft generation failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "AI_ERROR", "Failed to generate AI draft: "+err.Error())
		return
	}

	slog.Info("AI draft generated",
		"model", resp.Model,
		"tokens_in", resp.TokensIn,
		"tokens_out", resp.TokensOut,
		"latency_ms", resp.LatencyMs,
	)

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"body":       resp.Content,
		"model":      resp.Model,
		"tokens_in":  resp.TokensIn,
		"tokens_out": resp.TokensOut,
		"latency_ms": resp.LatencyMs,
	})
}
