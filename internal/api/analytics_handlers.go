package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// AnalyticsHandlers contains HTTP handlers for analytics endpoints.
type AnalyticsHandlers struct {
	pool *pgxpool.Pool
}

// NewAnalyticsHandlers creates a new AnalyticsHandlers instance.
func NewAnalyticsHandlers(pool *pgxpool.Pool) *AnalyticsHandlers {
	return &AnalyticsHandlers{pool: pool}
}

// formatTokens formats a token count into a human-readable string (e.g. 1.2K, 5.3M).
func formatTokens(count int64) string {
	switch {
	case count >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(count)/1_000_000_000)
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fK", float64(count)/1_000)
	default:
		return fmt.Sprintf("%d", count)
	}
}

// HandleAnalyticsUsage handles GET /api/v1/analytics/usage?period=today|month.
// Returns real usage data from ai_audit_log grouped by task_type and model_id.
func (h *AnalyticsHandlers) HandleAnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	period := r.URL.Query().Get("period")
	if period != "month" {
		period = "today"
	}

	var since time.Time
	now := time.Now()
	if period == "today" {
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	} else {
		since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}

	// --- Process breakdown from ai_audit_log ---
	type processRow struct {
		Name     string `json:"name"`
		Model    string `json:"model"`
		Calls    int    `json:"calls"`
		Tokens   string `json:"tokens"`
		Quota    string `json:"quota"`
		Percent  int    `json:"percent"`
		Category string `json:"category"`
	}

	rows, err := conn.Query(tc,
		`SELECT COALESCE(task_type, 'unknown'), COALESCE(model_used, 'unknown'),
		        COUNT(*)::int, COALESCE(SUM(tokens_in + tokens_out), 0)::bigint
		 FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2
		 GROUP BY task_type, model_used
		 ORDER BY COUNT(*) DESC`,
		tc.UserID, since)
	if err != nil {
		slog.Error("query ai_audit_log processes", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer rows.Close()

	// Task type display name mapping
	taskNames := map[string]string{
		"categorize":         "Categorization",
		"summarize":          "Summarization",
		"draft_reply":        "Draft Replies (Standard)",
		"draft_premium":      "Draft Replies (Premium)",
		"priority":           "Priority Scoring",
		"tone":               "Tone Classification",
		"rag_embed":          "RAG Embeddings",
		"context_match":      "Context Brief Matching",
		"contact_enrichment": "Contact Enrichment",
		"pref_learn":         "Preference Learning",
		"pref_synth":         "Preference Synthesis",
		"behavior":           "Behavior Analysis",
		"wellness":           "Wellness Report",
		"task_extract":       "Task Extraction",
		"expense_extract":    "Expense Extraction",
	}

	var processes []processRow
	for rows.Next() {
		var taskType, modelID string
		var calls int
		var totalTokens int64
		if err := rows.Scan(&taskType, &modelID, &calls, &totalTokens); err != nil {
			slog.Error("scan process row", "error", err)
			continue
		}

		name := taskType
		if displayName, ok := taskNames[taskType]; ok {
			name = displayName
		}

		processes = append(processes, processRow{
			Name:     name,
			Model:    modelID,
			Calls:    calls,
			Tokens:   formatTokens(totalTokens),
			Quota:    "\u221e",
			Percent:  0,
			Category: "ai",
		})
	}
	if processes == nil {
		processes = []processRow{}
	}

	// --- Summary stats ---
	var totalTokens int64
	var aiCalls int
	var emailsProcessed int
	var accountCount int

	_ = conn.QueryRow(tc,
		`SELECT COALESCE(SUM(tokens_in + tokens_out), 0)::bigint, COUNT(*)::int
		 FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2`,
		tc.UserID, since).Scan(&totalTokens, &aiCalls)

	_ = conn.QueryRow(tc,
		`SELECT COUNT(DISTINCT email_id)::int
		 FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2 AND email_id IS NOT NULL`,
		tc.UserID, since).Scan(&emailsProcessed)

	_ = conn.QueryRow(tc,
		`SELECT COUNT(*)::int FROM user_accounts WHERE user_id = $1`,
		tc.UserID).Scan(&accountCount)

	// Determine plan limits for summary display
	var tokenLimitStr string
	var tokenLimitNum int64
	var emailLimitStr string
	var emailLimitNum int
	var accountLimitStr string
	var accountLimitNum int
	var tokenUnit, emailUnit string

	// Read tenant plan
	var plan string
	_ = conn.QueryRow(tc,
		`SELECT COALESCE(t.plan, 'free') FROM tenants t WHERE t.id = $1`,
		tc.TenantID).Scan(&plan)

	switch plan {
	case "estate":
		tokenLimitStr = "\u221e"
		tokenLimitNum = 0
		emailLimitStr = "\u221e"
		emailLimitNum = 0
		accountLimitStr = "\u221e"
		accountLimitNum = 0
	case "privy_council":
		tokenLimitNum = 2_000_000
		emailLimitNum = 1000
		accountLimitNum = 25
		tokenLimitStr = "2M"
		emailLimitStr = "1,000"
		accountLimitStr = "25"
	case "attache":
		tokenLimitNum = 500_000
		emailLimitNum = 500
		accountLimitNum = 10
		tokenLimitStr = "500K"
		emailLimitStr = "500"
		accountLimitStr = "10"
	default: // free
		tokenLimitNum = 50_000
		emailLimitNum = 100
		accountLimitNum = 1
		tokenLimitStr = "50K"
		emailLimitStr = "100"
		accountLimitStr = "1"
	}

	if period == "today" {
		tokenUnit = "/day"
		emailUnit = "/day"
	} else {
		tokenUnit = "/mo"
		emailUnit = "/mo"
	}

	tokenPercent := 0
	if tokenLimitNum > 0 {
		tokenPercent = int(float64(totalTokens) / float64(tokenLimitNum) * 100)
		if tokenPercent > 100 {
			tokenPercent = 100
		}
	}

	emailPercent := 0
	if emailLimitNum > 0 {
		emailPercent = emailsProcessed * 100 / emailLimitNum
		if emailPercent > 100 {
			emailPercent = 100
		}
	}

	accountPercent := 0
	if accountLimitNum > 0 {
		accountPercent = accountCount * 100 / accountLimitNum
		if accountPercent > 100 {
			accountPercent = 100
		}
	}

	type summaryCard struct {
		Label   string `json:"label"`
		Value   string `json:"value"`
		Limit   string `json:"limit"`
		Percent int    `json:"percent"`
		Unit    string `json:"unit"`
	}

	summary := []summaryCard{
		{
			Label:   "Total Tokens",
			Value:   formatTokens(totalTokens),
			Limit:   tokenLimitStr,
			Percent: tokenPercent,
			Unit:    tokenUnit,
		},
		{
			Label:   "AI Calls",
			Value:   fmt.Sprintf("%d", aiCalls),
			Limit:   "\u221e",
			Percent: 100,
			Unit:    "",
		},
		{
			Label:   "Emails Processed",
			Value:   fmt.Sprintf("%d", emailsProcessed),
			Limit:   emailLimitStr,
			Percent: emailPercent,
			Unit:    emailUnit,
		},
		{
			Label:   "Accounts",
			Value:   fmt.Sprintf("%d", accountCount),
			Limit:   accountLimitStr,
			Percent: accountPercent,
			Unit:    "",
		},
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"processes": processes,
		"summary":   summary,
	})
}

// HandleMemoryHealth handles GET /api/v1/analytics/memory-health.
func (h *AnalyticsHandlers) HandleMemoryHealth(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("memory health: acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("memory health: set rls", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	var learnedPatterns, userCorrections, activeRules int
	var lastUpdated *time.Time

	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM learned_patterns WHERE user_id = $1 AND confidence >= 70`,
		tc.UserID).Scan(&learnedPatterns)

	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM preference_signals WHERE user_id = $1`,
		tc.UserID).Scan(&userCorrections)

	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM user_rules WHERE user_id = $1 AND active = true`,
		tc.UserID).Scan(&activeRules)

	var ts time.Time
	err = conn.QueryRow(tc,
		`SELECT MAX(created_at) FROM learned_patterns WHERE user_id = $1`,
		tc.UserID).Scan(&ts)
	if err == nil && !ts.IsZero() {
		lastUpdated = &ts
	}

	var lastUpdatedStr *string
	if lastUpdated != nil {
		s := lastUpdated.Format(time.RFC3339)
		lastUpdatedStr = &s
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"learned_patterns":  learnedPatterns,
		"user_corrections":  userCorrections,
		"active_rules":      activeRules,
		"last_updated":      lastUpdatedStr,
	})
}
