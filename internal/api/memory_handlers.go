package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// MemoryHandlers contains HTTP handlers for AI memory (user rules, context briefs, learned patterns).
type MemoryHandlers struct {
	pool *pgxpool.Pool
}

// NewMemoryHandlers creates a new MemoryHandlers instance.
func NewMemoryHandlers(pool *pgxpool.Pool) *MemoryHandlers {
	return &MemoryHandlers{pool: pool}
}

// ---------- User Rules ----------

// HandleListUserRules handles GET /api/v1/user-rules.
func (h *MemoryHandlers) HandleListUserRules(w http.ResponseWriter, r *http.Request) {
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

	rows, err := conn.Query(tc,
		`SELECT id, scope, type, text, COALESCE(contact_filter, ''), active, created_at
		 FROM user_rules WHERE user_id = $1 ORDER BY created_at DESC`, tc.UserID)
	if err != nil {
		slog.Error("query user rules", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load rules")
		return
	}
	defer rows.Close()

	type ruleRow struct {
		ID            string `json:"id"`
		Scope         string `json:"scope"`
		Type          string `json:"type"`
		Text          string `json:"text"`
		Instruction   string `json:"instruction"`
		ContactFilter string `json:"contact_filter"`
		Active        bool   `json:"active"`
		CreatedAt     string `json:"created_at"`
	}
	rules := []ruleRow{}
	for rows.Next() {
		var row ruleRow
		var createdAt time.Time
		if err := rows.Scan(&row.ID, &row.Scope, &row.Type, &row.Text, &row.ContactFilter, &row.Active, &createdAt); err != nil {
			slog.Error("scan rule row", "error", err)
			continue
		}
		row.CreatedAt = createdAt.Format(time.RFC3339)
		row.Instruction = row.Text // Frontend expects "instruction"
		rules = append(rules, row)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterate user rules", "error", err)
	}

	WriteJSON(w, r, http.StatusOK, rules)
}

// HandleCreateUserRule handles POST /api/v1/user-rules.
func (h *MemoryHandlers) HandleCreateUserRule(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Scope         string `json:"scope"`
		Type          string `json:"type"`
		Text          string `json:"text"`
		Instruction   string `json:"instruction"`
		ContactFilter string `json:"contact_filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}
	// Accept both "text" and "instruction" field names
	if req.Text == "" && req.Instruction != "" {
		req.Text = req.Instruction
	}
	if req.Scope == "" || req.Type == "" || req.Text == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "scope, type, and text/instruction are required")
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

	var id string
	var createdAt time.Time
	err = conn.QueryRow(tc,
		`INSERT INTO user_rules (user_id, tenant_id, scope, type, text, contact_filter)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		 RETURNING id, created_at`,
		tc.UserID, tc.TenantID, req.Scope, req.Type, req.Text, req.ContactFilter).
		Scan(&id, &createdAt)
	if err != nil {
		slog.Error("insert user rule", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create rule")
		return
	}

	WriteJSON(w, r, http.StatusCreated, map[string]interface{}{
		"id":             id,
		"scope":          req.Scope,
		"type":           req.Type,
		"text":           req.Text,
		"instruction":    req.Text,
		"contact_filter": req.ContactFilter,
		"active":         true,
		"created_at":     createdAt.Format(time.RFC3339),
	})
}

// HandleUpdateUserRule handles PUT /api/v1/user-rules/{id}.
func (h *MemoryHandlers) HandleUpdateUserRule(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	ruleID := chi.URLParam(r, "id")
	if ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing rule id")
		return
	}

	var req struct {
		Scope         *string `json:"scope"`
		Type          *string `json:"type"`
		Text          *string `json:"text"`
		ContactFilter *string `json:"contact_filter"`
		Active        *bool   `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	// Build dynamic update
	sets := []string{"updated_at = NOW()"}
	args := []interface{}{}
	idx := 1

	if req.Scope != nil {
		sets = append(sets, fmt.Sprintf("scope = $%d", idx))
		args = append(args, *req.Scope)
		idx++
	}
	if req.Type != nil {
		sets = append(sets, fmt.Sprintf("type = $%d", idx))
		args = append(args, *req.Type)
		idx++
	}
	if req.Text != nil {
		sets = append(sets, fmt.Sprintf("text = $%d", idx))
		args = append(args, *req.Text)
		idx++
	}
	if req.ContactFilter != nil {
		sets = append(sets, fmt.Sprintf("contact_filter = NULLIF($%d, '')", idx))
		args = append(args, *req.ContactFilter)
		idx++
	}
	if req.Active != nil {
		sets = append(sets, fmt.Sprintf("active = $%d", idx))
		args = append(args, *req.Active)
		idx++
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

	args = append(args, ruleID, tc.UserID)
	query := fmt.Sprintf("UPDATE user_rules SET %s WHERE id = $%d AND user_id = $%d",
		strings.Join(sets, ", "), idx, idx+1)
	tag, err := conn.Exec(tc, query, args...)
	if err != nil {
		slog.Error("update user rule", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update rule")
		return
	}
	if tag.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "rule not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "rule updated"})
}

// HandleDeleteUserRule handles DELETE /api/v1/user-rules/{id}.
func (h *MemoryHandlers) HandleDeleteUserRule(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	ruleID := chi.URLParam(r, "id")
	if ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing rule id")
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

	tag, err := conn.Exec(tc,
		`DELETE FROM user_rules WHERE id = $1 AND user_id = $2`, ruleID, tc.UserID)
	if err != nil {
		slog.Error("delete user rule", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete rule")
		return
	}
	if tag.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "rule not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "rule deleted"})
}

// ---------- Context Briefs ----------

// HandleListContextBriefs handles GET /api/v1/context-briefs.
func (h *MemoryHandlers) HandleListContextBriefs(w http.ResponseWriter, r *http.Request) {
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

	rows, err := conn.Query(tc,
		`SELECT id, title, scope, text, COALESCE(keywords, '[]'::jsonb), expires_at, created_at
		 FROM context_briefs WHERE user_id = $1
		 ORDER BY created_at DESC`, tc.UserID)
	if err != nil {
		slog.Error("query context briefs", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load context briefs")
		return
	}
	defer rows.Close()

	type briefRow struct {
		ID        string      `json:"id"`
		Title     string      `json:"title"`
		Scope     string      `json:"scope"`
		Content   string      `json:"content"`
		Context   string      `json:"context"`
		Keywords  interface{} `json:"keywords"`
		ExpiresAt *string     `json:"expires_at"`
		CreatedAt string      `json:"created_at"`
	}
	briefs := []briefRow{}
	for rows.Next() {
		var row briefRow
		var keywordsJSON []byte
		var expiresAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(&row.ID, &row.Title, &row.Scope, &row.Content, &keywordsJSON, &expiresAt, &createdAt); err != nil {
			slog.Error("scan context brief row", "error", err)
			continue
		}
		row.CreatedAt = createdAt.Format(time.RFC3339)
		if expiresAt != nil {
			formatted := expiresAt.Format(time.RFC3339)
			row.ExpiresAt = &formatted
		}
		// Parse keywords JSON into a proper array
		var keywords interface{}
		if err := json.Unmarshal(keywordsJSON, &keywords); err != nil {
			keywords = []string{}
		}
		row.Keywords = keywords
		row.Context = row.Content // Frontend expects "context"
		briefs = append(briefs, row)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterate context briefs", "error", err)
	}

	WriteJSON(w, r, http.StatusOK, briefs)
}

// HandleCreateContextBrief handles POST /api/v1/context-briefs.
func (h *MemoryHandlers) HandleCreateContextBrief(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Title     string   `json:"title"`
		Scope     string   `json:"scope"`
		Content   string   `json:"content"`
		Context   string   `json:"context"`
		Keywords  []string `json:"keywords"`
		ExpiresAt *string  `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}
	// Accept both "content" and "context" field names
	if req.Content == "" && req.Context != "" {
		req.Content = req.Context
	}
	if req.Title == "" || req.Scope == "" || req.Content == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "title, scope, and content are required")
		return
	}

	// Marshal keywords to JSONB
	keywordsJSON, err := json.Marshal(req.Keywords)
	if err != nil {
		keywordsJSON = []byte("[]")
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

	var id string
	var createdAt time.Time
	err = conn.QueryRow(tc,
		`INSERT INTO context_briefs (user_id, tenant_id, title, scope, text, keywords, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::timestamptz)
		 RETURNING id, created_at`,
		tc.UserID, tc.TenantID, req.Title, req.Scope, req.Content, string(keywordsJSON), req.ExpiresAt).
		Scan(&id, &createdAt)
	if err != nil {
		slog.Error("insert context brief", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create context brief")
		return
	}

	WriteJSON(w, r, http.StatusCreated, map[string]interface{}{
		"id":         id,
		"title":      req.Title,
		"scope":      req.Scope,
		"content":    req.Content,
		"keywords":   req.Keywords,
		"expires_at": req.ExpiresAt,
		"created_at": createdAt.Format(time.RFC3339),
	})
}

// HandleDeleteContextBrief handles DELETE /api/v1/context-briefs/{id}.
func (h *MemoryHandlers) HandleDeleteContextBrief(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	briefID := chi.URLParam(r, "id")
	if briefID == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing brief id")
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

	tag, err := conn.Exec(tc,
		`DELETE FROM context_briefs WHERE id = $1 AND user_id = $2`, briefID, tc.UserID)
	if err != nil {
		slog.Error("delete context brief", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete context brief")
		return
	}
	if tag.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "context brief not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "context brief deleted"})
}

// ---------- Learned Patterns ----------

// HandleListLearnedPatterns handles GET /api/v1/learned-patterns.
func (h *MemoryHandlers) HandleListLearnedPatterns(w http.ResponseWriter, r *http.Request) {
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

	rows, err := conn.Query(tc,
		`SELECT id, pattern_text, confidence, COALESCE(source_description, ''), category, created_at
		 FROM learned_patterns WHERE user_id = $1
		 ORDER BY confidence DESC, created_at DESC`, tc.UserID)
	if err != nil {
		slog.Error("query learned patterns", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load learned patterns")
		return
	}
	defer rows.Close()

	type patternRow struct {
		ID         string `json:"id"`
		Pattern    string `json:"pattern"`
		Confidence int    `json:"confidence"`
		Source     string `json:"source"`
		DataSource string `json:"data_source"`
		Category   string `json:"category"`
		CreatedAt  string `json:"created_at"`
	}
	patterns := []patternRow{}
	for rows.Next() {
		var row patternRow
		var createdAt time.Time
		if err := rows.Scan(&row.ID, &row.Pattern, &row.Confidence, &row.Source, &row.Category, &createdAt); err != nil {
			slog.Error("scan learned pattern row", "error", err)
			continue
		}
		row.CreatedAt = createdAt.Format(time.RFC3339)
		row.DataSource = row.Source // Frontend expects "data_source"
		patterns = append(patterns, row)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterate learned patterns", "error", err)
	}

	WriteJSON(w, r, http.StatusOK, patterns)
}

// HandleGeneratePatterns handles POST /api/v1/learned-patterns/generate.
// Analyzes existing email data to derive communication patterns.
func (h *MemoryHandlers) HandleGeneratePatterns(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("generate patterns: acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("generate patterns: set rls", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Clear existing patterns for regeneration
	_, _ = conn.Exec(tc, `DELETE FROM learned_patterns WHERE user_id = $1`, tc.UserID)

	type pattern struct {
		Category    string
		PatternText string
		Confidence  int
		Evidence    int
		Source      string
	}
	var patterns []pattern

	// 1. Communication pattern: top senders
	type senderStat struct {
		FromAddress string
		FromName    string
		Count       int
	}
	senderRows, err := conn.Query(tc,
		`SELECT from_address, COALESCE(from_name, ''), COUNT(*) as cnt
		 FROM emails WHERE user_id = $1 AND direction = 'inbound'
		 GROUP BY from_address, from_name
		 HAVING COUNT(*) >= 3
		 ORDER BY cnt DESC LIMIT 5`, tc.UserID)
	if err == nil {
		defer senderRows.Close()
		for senderRows.Next() {
			var s senderStat
			if senderRows.Scan(&s.FromAddress, &s.FromName, &s.Count) == nil {
				name := s.FromName
				if name == "" {
					name = s.FromAddress
				}
				patterns = append(patterns, pattern{
					Category:    "communication",
					PatternText: fmt.Sprintf("Frequent sender: %s (%d emails)", name, s.Count),
					Confidence:  min(90, 50+s.Count*5),
					Evidence:    s.Count,
					Source:      "email_frequency_analysis",
				})
			}
		}
	}

	// 2. Priority pattern: category distribution
	type catStat struct {
		Category string
		Count    int
	}
	catRows, err := conn.Query(tc,
		`SELECT ec.category, COUNT(*) as cnt
		 FROM email_categories ec
		 JOIN emails e ON e.id = ec.email_id
		 WHERE e.user_id = $1
		 GROUP BY ec.category
		 HAVING COUNT(*) >= 2
		 ORDER BY cnt DESC LIMIT 5`, tc.UserID)
	if err == nil {
		defer catRows.Close()
		for catRows.Next() {
			var c catStat
			if catRows.Scan(&c.Category, &c.Count) == nil {
				patterns = append(patterns, pattern{
					Category:    "priority",
					PatternText: fmt.Sprintf("Email category '%s' appears frequently (%d emails)", c.Category, c.Count),
					Confidence:  min(85, 40+c.Count*3),
					Evidence:    c.Count,
					Source:      "category_distribution_analysis",
				})
			}
		}
	}

	// 3. Reply pattern: draft acceptance/rejection rate
	var approved, rejected, pending int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FILTER (WHERE status = 'approved'),
		        COUNT(*) FILTER (WHERE status = 'rejected'),
		        COUNT(*) FILTER (WHERE status = 'pending')
		 FROM draft_replies WHERE user_id = $1`, tc.UserID).Scan(&approved, &rejected, &pending)

	total := approved + rejected
	if total > 0 {
		rate := (approved * 100) / total
		patterns = append(patterns, pattern{
			Category:    "reply",
			PatternText: fmt.Sprintf("AI draft acceptance rate: %d%% (%d approved, %d rejected)", rate, approved, rejected),
			Confidence:  min(90, 50+total*2),
			Evidence:    total,
			Source:      "draft_acceptance_analysis",
		})
	}
	if pending > 0 {
		patterns = append(patterns, pattern{
			Category:    "reply",
			PatternText: fmt.Sprintf("%d drafts pending review", pending),
			Confidence:  80,
			Evidence:    pending,
			Source:      "draft_queue_analysis",
		})
	}

	// 4. Schedule pattern: email volume by hour
	type hourStat struct {
		Hour  int
		Count int
	}
	hourRows, err := conn.Query(tc,
		`SELECT EXTRACT(HOUR FROM received_at)::int as hr, COUNT(*) as cnt
		 FROM emails WHERE user_id = $1 AND direction = 'inbound'
		 GROUP BY hr ORDER BY cnt DESC LIMIT 3`, tc.UserID)
	if err == nil {
		defer hourRows.Close()
		for hourRows.Next() {
			var hs hourStat
			if hourRows.Scan(&hs.Hour, &hs.Count) == nil {
				period := "morning"
				if hs.Hour >= 12 && hs.Hour < 17 {
					period = "afternoon"
				} else if hs.Hour >= 17 {
					period = "evening"
				}
				patterns = append(patterns, pattern{
					Category:    "schedule",
					PatternText: fmt.Sprintf("Peak email volume at %d:00 (%s, %d emails)", hs.Hour, period, hs.Count),
					Confidence:  min(80, 40+hs.Count*2),
					Evidence:    hs.Count,
					Source:      "email_timing_analysis",
				})
			}
		}
	}

	// Insert all patterns
	for _, p := range patterns {
		_, err := conn.Exec(tc,
			`INSERT INTO learned_patterns (user_id, tenant_id, category, pattern_text, confidence, evidence_count, source_description, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, now())`,
			tc.UserID, tc.TenantID, p.Category, p.PatternText, p.Confidence, p.Evidence, p.Source)
		if err != nil {
			slog.Error("generate patterns: insert", "error", err, "pattern", p.PatternText)
		}
	}

	slog.Info("generate patterns: complete", "user_id", tc.UserID, "count", len(patterns))
	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"generated": len(patterns),
	})
}
