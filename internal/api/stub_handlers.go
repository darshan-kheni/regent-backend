package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// planTokenLimits maps plan names to daily token limits.
var planTokenLimits = map[string]int{
	"free":          50000,
	"attache":       500000,
	"privy_council": 2000000,
	"estate":        0, // unlimited
}

// HandleAnalytics handles GET /api/v1/analytics.
func (h *AnalyticsHandlers) HandleAnalytics(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	period := r.URL.Query().Get("period")

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("analytics: acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("analytics: set rls", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Use user's timezone for date calculations
	var analyticsTZ string
	_ = conn.QueryRow(tc, `SELECT COALESCE(u.timezone, 'UTC') FROM users u WHERE u.id = $1`, tc.UserID).Scan(&analyticsTZ)
	aLoc, aLocErr := time.LoadLocation(analyticsTZ)
	if aLocErr != nil {
		aLoc = time.UTC
	}
	now := time.Now().In(aLoc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, aLoc).UTC()
	var since time.Time
	switch period {
	case "week":
		since = todayStart.AddDate(0, 0, -7)
	case "month":
		since = todayStart.AddDate(0, -1, 0)
	default: // "today"
		since = todayStart
	}

	// Total tokens used in period
	var tokensUsed int64
	_ = conn.QueryRow(tc,
		`SELECT COALESCE(SUM(tokens_in + tokens_out), 0) FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2`, tc.UserID, since).Scan(&tokensUsed)

	// Total AI calls in period
	var aiCalls int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2`, tc.UserID, since).Scan(&aiCalls)

	// Average latency in period
	var avgLatency float64
	_ = conn.QueryRow(tc,
		`SELECT COALESCE(AVG(latency_ms), 0) FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2 AND latency_ms > 0`, tc.UserID, since).Scan(&avgLatency)

	// Cache hit rate in period
	var cacheHits, cacheTotalCalls int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FILTER (WHERE cache_hit = true), COUNT(*)
		 FROM ai_audit_log WHERE user_id = $1 AND created_at >= $2`, tc.UserID, since).Scan(&cacheHits, &cacheTotalCalls)
	var cacheHitRate float64
	if cacheTotalCalls > 0 {
		cacheHitRate = float64(cacheHits) / float64(cacheTotalCalls) * 100
	}

	// Draft acceptance rate
	var draftsApproved, draftsTotal int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FILTER (WHERE status = 'approved'), COUNT(*)
		 FROM draft_replies WHERE user_id = $1 AND created_at >= $2`, tc.UserID, since).Scan(&draftsApproved, &draftsTotal)
	var draftAcceptRate float64
	if draftsTotal > 0 {
		draftAcceptRate = float64(draftsApproved) / float64(draftsTotal) * 100
	}

	// Get plan name and token limit
	var planName string
	_ = conn.QueryRow(tc,
		`SELECT COALESCE(t.plan, 'free') FROM tenants t
		 JOIN users u ON u.tenant_id = t.id WHERE u.id = $1`, tc.UserID).Scan(&planName)
	tokensLimit := planTokenLimits[planName]
	if tokensLimit == 0 && planName == "estate" {
		tokensLimit = 999999999 // unlimited display
	}
	if tokensLimit == 0 {
		tokensLimit = 50000
	}

	// 7-day trend
	type trendPoint struct {
		Date   string `json:"date"`
		Tokens int64  `json:"tokens"`
	}
	var trend []trendPoint
	trendRows, trendErr := conn.Query(tc,
		`SELECT d::date as day, COALESCE(SUM(a.tokens_in + a.tokens_out), 0) as tokens
		 FROM generate_series(CURRENT_DATE - interval '6 days', CURRENT_DATE, '1 day') d
		 LEFT JOIN ai_audit_log a ON a.user_id = $1 AND a.created_at::date = d::date
		 GROUP BY d::date ORDER BY d::date`, tc.UserID)
	if trendErr == nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var tp trendPoint
			var day time.Time
			if trendRows.Scan(&day, &tp.Tokens) == nil {
				tp.Date = day.Format("2006-01-02")
				trend = append(trend, tp)
			}
		}
	}
	if trend == nil {
		trend = []trendPoint{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"tokens_used":           tokensUsed,
		"tokens_limit":          tokensLimit,
		"plan_name":             planName,
		"ai_calls":              aiCalls,
		"avg_latency_ms":        int(avgLatency),
		"cache_hit_rate":        int(cacheHitRate),
		"draft_acceptance_rate": int(draftAcceptRate),
		"trend":                 trend,
	})
}

// HandleAnalyticsServices handles GET /api/v1/analytics/services.
func (h *AnalyticsHandlers) HandleAnalyticsServices(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	period := r.URL.Query().Get("period")

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("analytics services: acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("analytics services: set rls", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	var svcTZ string
	_ = conn.QueryRow(tc, `SELECT COALESCE(u.timezone, 'UTC') FROM users u WHERE u.id = $1`, tc.UserID).Scan(&svcTZ)
	sLoc, sLocErr := time.LoadLocation(svcTZ)
	if sLocErr != nil {
		sLoc = time.UTC
	}
	sNow := time.Now().In(sLoc)
	sTodayStart := time.Date(sNow.Year(), sNow.Month(), sNow.Day(), 0, 0, 0, 0, sLoc).UTC()
	var since time.Time
	switch period {
	case "week":
		since = sTodayStart.AddDate(0, 0, -7)
	case "month":
		since = sTodayStart.AddDate(0, -1, 0)
	default:
		since = sTodayStart
	}

	type serviceRow struct {
		ServiceName string  `json:"service_name"`
		Model       string  `json:"model"`
		Tokens      int64   `json:"tokens"`
		Calls       int     `json:"calls"`
		AvgLatency  int     `json:"avg_latency_ms"`
		UsagePercent float64 `json:"usage_percent"`
	}

	// Task type to display name mapping
	taskNames := map[string]string{
		"categorize":  "Email Categorization",
		"summarize":   "Email Summarization",
		"draft_reply": "Draft Replies",
	}

	rows, err := conn.Query(tc,
		`SELECT task_type, COALESCE(model_used, 'unknown'),
		        COALESCE(SUM(tokens_in + tokens_out), 0) as total_tokens,
		        COUNT(*) as calls,
		        COALESCE(AVG(latency_ms)::int, 0) as avg_latency
		 FROM ai_audit_log
		 WHERE user_id = $1 AND created_at >= $2
		 GROUP BY task_type, model_used
		 ORDER BY total_tokens DESC`, tc.UserID, since)
	if err != nil {
		slog.Error("analytics services: query", "error", err)
		WriteJSON(w, r, http.StatusOK, []serviceRow{})
		return
	}
	defer rows.Close()

	var services []serviceRow
	var grandTotal int64
	// First pass: collect data
	type rawRow struct {
		taskType   string
		model      string
		tokens     int64
		calls      int
		avgLatency int
	}
	var rawRows []rawRow
	for rows.Next() {
		var rr rawRow
		if rows.Scan(&rr.taskType, &rr.model, &rr.tokens, &rr.calls, &rr.avgLatency) == nil {
			rawRows = append(rawRows, rr)
			grandTotal += rr.tokens
		}
	}

	for _, rr := range rawRows {
		name := taskNames[rr.taskType]
		if name == "" {
			name = rr.taskType
		}
		var pct float64
		if grandTotal > 0 {
			pct = float64(rr.tokens) / float64(grandTotal) * 100
		}
		services = append(services, serviceRow{
			ServiceName:  name,
			Model:        rr.model,
			Tokens:       rr.tokens,
			Calls:        rr.calls,
			AvgLatency:   rr.avgLatency,
			UsagePercent: float64(int(pct*10)) / 10, // round to 1 decimal
		})
	}

	if services == nil {
		services = []serviceRow{}
	}

	WriteJSON(w, r, http.StatusOK, services)
}

// HandleComposeSend is a legacy stub — real implementation is in send_handlers.go.

// HandleComposeAiDraft is a legacy stub — real implementation is in compose_handlers.go.

// moduleServiceDef defines a static service configuration.
type moduleServiceDef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Icon        string `json:"icon"`
	Model       string `json:"model,omitempty"`
	MinPlan     string `json:"min_plan"` // free, attache, privy_council, estate
}

// allServiceDefs is the product-level service catalog.
var allServiceDefs = []moduleServiceDef{
	// Email Processing
	{ID: "email_fetch", Name: "Email Fetching", Description: "IMAP/Gmail API real-time email sync", Group: "email_processing", Icon: "mail", MinPlan: "free"},
	{ID: "email_send", Name: "Email Sending", Description: "SMTP/Gmail API outbound delivery", Group: "email_processing", Icon: "send", MinPlan: "free"},
	{ID: "categorize", Name: "Categorization", Description: "AI email classification", Group: "email_processing", Icon: "layers", Model: "gemma3:4b", MinPlan: "free"},
	{ID: "prioritize", Name: "Priority Scoring", Description: "Urgency and importance ranking", Group: "email_processing", Icon: "zap", Model: "gemma3:4b", MinPlan: "free"},
	{ID: "tone", Name: "Tone Classification", Description: "Detect email tone and sentiment", Group: "email_processing", Icon: "message-circle", Model: "gemma3:4b", MinPlan: "attache"},
	{ID: "summarize", Name: "Summarization", Description: "Executive email briefs", Group: "email_processing", Icon: "file-text", Model: "ministral-3:8b", MinPlan: "attache"},
	// Intelligence
	{ID: "rag", Name: "RAG Embeddings", Description: "Vector search for contextual AI responses", Group: "intelligence", Icon: "database", Model: "nomic-embed-text", MinPlan: "attache"},
	{ID: "context_match", Name: "Context Brief Matching", Description: "Match emails to active context briefs", Group: "intelligence", Icon: "link", MinPlan: "attache"},
	{ID: "contact_enrich", Name: "Contact Enrichment", Description: "Auto-extract contact info from signatures", Group: "intelligence", Icon: "users", Model: "gemma3:4b", MinPlan: "attache"},
	{ID: "draft_standard", Name: "Standard Draft Replies", Description: "AI-composed reply suggestions", Group: "intelligence", Icon: "edit", Model: "gemma3:12b", MinPlan: "attache"},
	{ID: "draft_premium", Name: "Premium Draft Replies", Description: "High-quality drafts for sensitive emails", Group: "intelligence", Icon: "star", Model: "gpt-oss:120b", MinPlan: "privy_council"},
	{ID: "pref_learn", Name: "Preference Learning", Description: "Learn from your corrections and feedback", Group: "intelligence", Icon: "brain", Model: "ministral-3:8b", MinPlan: "attache"},
	{ID: "pref_synth", Name: "Preference Synthesis", Description: "Weekly behavior model update", Group: "intelligence", Icon: "cpu", Model: "gpt-oss:120b", MinPlan: "privy_council"},
	// AI Behavior
	{ID: "behavior", Name: "Behavior Intelligence", Description: "Communication patterns and productivity insights", Group: "ai_behavior", Icon: "activity", Model: "ministral-3:8b", MinPlan: "attache"},
	{ID: "wellness", Name: "Wellness Reporting", Description: "Weekly work-life balance assessments", Group: "ai_behavior", Icon: "heart", Model: "gpt-oss:120b", MinPlan: "privy_council"},
	// Notifications
	{ID: "briefing", Name: "Briefing Delivery", Description: "Multi-channel digest and alert delivery", Group: "notifications", Icon: "bell", MinPlan: "free"},
	{ID: "whatsapp", Name: "WhatsApp / Signal", Description: "Encrypted messaging channel integration", Group: "notifications", Icon: "message-square", MinPlan: "attache"},
}

var planOrder = map[string]int{"free": 0, "attache": 1, "privy_council": 2, "estate": 3}

// HandleListModuleServices handles GET /api/v1/modules/services.
// Returns service catalog with user toggle prefs and plan-based availability.
func (h *AnalyticsHandlers) HandleListModuleServices(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Get user plan
	var plan string
	_ = conn.QueryRow(tc,
		`SELECT COALESCE(t.plan, 'free') FROM tenants t JOIN users u ON u.tenant_id = t.id WHERE u.id = $1`,
		tc.UserID).Scan(&plan)

	// Get user toggle preferences
	prefs := map[string]bool{}
	rows, err := conn.Query(tc,
		`SELECT service_id, enabled FROM user_module_prefs WHERE user_id = $1`, tc.UserID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var svcID string
			var enabled bool
			if rows.Scan(&svcID, &enabled) == nil {
				prefs[svcID] = enabled
			}
		}
	}

	userPlanLevel := planOrder[plan]

	type serviceResponse struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Group       string `json:"group"`
		Icon        string `json:"icon"`
		Model       string `json:"model,omitempty"`
		Enabled     bool   `json:"enabled"`
		Status      string `json:"status"`
		MinPlan     string `json:"min_plan"`
		Locked      bool   `json:"locked"`
	}

	var services []serviceResponse
	for _, def := range allServiceDefs {
		minLevel := planOrder[def.MinPlan]
		locked := userPlanLevel < minLevel

		// Determine enabled state
		enabled := !locked // default: enabled if plan allows
		if pref, hasPref := prefs[def.ID]; hasPref {
			enabled = pref && !locked
		}

		// Determine status
		status := "active"
		if locked {
			status = "locked"
			enabled = false
		} else if !enabled {
			status = "disabled"
		}

		services = append(services, serviceResponse{
			ID:          def.ID,
			Name:        def.Name,
			Description: def.Description,
			Group:       def.Group,
			Icon:        def.Icon,
			Model:       def.Model,
			Enabled:     enabled,
			Status:      status,
			MinPlan:     def.MinPlan,
			Locked:      locked,
		})
	}

	WriteJSON(w, r, http.StatusOK, services)
}

// HandleUpdateModuleService handles PUT /api/v1/modules/services/{id}.
// Persists user toggle preference to the database.
func (h *AnalyticsHandlers) HandleUpdateModuleService(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	serviceID := chi.URLParam(r, "id")
	if serviceID == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing service id")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	_, err = conn.Exec(tc,
		`INSERT INTO user_module_prefs (user_id, tenant_id, service_id, enabled)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, service_id) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now()`,
		tc.UserID, tc.TenantID, serviceID, req.Enabled,
	)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update preference")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"status": "updated", "service_id": serviceID, "enabled": req.Enabled})
}
