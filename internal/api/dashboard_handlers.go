package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// DashboardHandlers contains HTTP handlers for the dashboard.
type DashboardHandlers struct {
	pool *pgxpool.Pool
}

// NewDashboardHandlers creates a new DashboardHandlers instance.
func NewDashboardHandlers(pool *pgxpool.Pool) *DashboardHandlers {
	return &DashboardHandlers{pool: pool}
}

// HandleDashboardStats handles GET /api/v1/dashboard/stats.
func (h *DashboardHandlers) HandleDashboardStats(w http.ResponseWriter, r *http.Request) {
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

	// Use user's timezone for "today" calculation
	var userTZ string
	_ = conn.QueryRow(tc, `SELECT COALESCE(u.timezone, 'UTC') FROM users u WHERE u.id = $1`, tc.UserID).Scan(&userTZ)
	loc, locErr := time.LoadLocation(userTZ)
	if locErr != nil {
		loc = time.UTC
	}
	nowUser := time.Now().In(loc)
	today := time.Date(nowUser.Year(), nowUser.Month(), nowUser.Day(), 0, 0, 0, 0, loc).UTC()

	var emailsToday, emailsTotal, draftsToday, aiProcessed, pendingReplies, activeConnections int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM emails WHERE user_id = $1 AND created_at >= $2`,
		tc.UserID, today).Scan(&emailsToday)
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM emails WHERE user_id = $1`,
		tc.UserID).Scan(&emailsTotal)
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM draft_replies WHERE user_id = $1 AND created_at >= $2`,
		tc.UserID, today).Scan(&draftsToday)
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM email_ai_status WHERE user_id = $1 AND stage = 'complete'`,
		tc.UserID).Scan(&aiProcessed)
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM draft_replies WHERE user_id = $1 AND status = 'pending'`,
		tc.UserID).Scan(&pendingReplies)
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM user_accounts WHERE user_id = $1 AND sync_status = 'active'`,
		tc.UserID).Scan(&activeConnections)

	// Average AI processing time in seconds (from audit log)
	// This shows how fast Regent processes emails end-to-end
	var avgResponse *float64
	var avgVal float64
	err = conn.QueryRow(tc,
		`SELECT AVG(latency_ms) / 1000.0 FROM ai_audit_log
		 WHERE user_id = $1 AND latency_ms > 0`,
		tc.UserID).Scan(&avgVal)
	if err == nil && avgVal > 0 {
		rounded := float64(int(avgVal*10)) / 10
		avgResponse = &rounded
	}

	// Category distribution from email_categories
	type catCount struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	}
	var categories []catCount
	catRows, catErr := conn.Query(tc,
		`SELECT COALESCE(LOWER(ec.primary_category), 'uncategorized'), COUNT(*)
		 FROM emails e LEFT JOIN email_categories ec ON e.id = ec.email_id
		 WHERE e.user_id = $1
		 GROUP BY LOWER(ec.primary_category) ORDER BY COUNT(*) DESC`, tc.UserID)
	if catErr == nil {
		defer catRows.Close()
		for catRows.Next() {
			var c catCount
			if catRows.Scan(&c.Category, &c.Count) == nil {
				categories = append(categories, c)
			}
		}
	}
	if categories == nil {
		categories = []catCount{}
	}

	// Connected accounts
	type connectedAccount struct {
		ID           string `json:"id"`
		EmailAddress string `json:"email_address"`
		DisplayName  string `json:"display_name"`
		Provider     string `json:"provider"`
		SyncStatus   string `json:"sync_status"`
	}
	var connAccounts []connectedAccount
	accRows, accErr := conn.Query(tc,
		`SELECT id, email_address, COALESCE(display_name, ''), provider, COALESCE(sync_status, 'unknown')
		 FROM user_accounts WHERE user_id = $1 ORDER BY created_at`, tc.UserID)
	if accErr == nil {
		defer accRows.Close()
		for accRows.Next() {
			var a connectedAccount
			if accRows.Scan(&a.ID, &a.EmailAddress, &a.DisplayName, &a.Provider, &a.SyncStatus) == nil {
				connAccounts = append(connAccounts, a)
			}
		}
	}
	if connAccounts == nil {
		connAccounts = []connectedAccount{}
	}

	// Requires attention: high-priority inbound emails without drafts, last 7 days
	type attentionEmail struct {
		ID          string `json:"id"`
		Subject     string `json:"subject"`
		FromName    string `json:"from_name"`
		FromAddress string `json:"from_address"`
		ReceivedAt  string `json:"received_at"`
		Priority    int    `json:"priority"`
		Category    string `json:"category"`
		AccountID   string `json:"account_id"`
		Snippet     string `json:"snippet"`
	}
	var attention []attentionEmail
	weekAgo := time.Now().Add(-7 * 24 * time.Hour)
	attRows, attErr := conn.Query(tc,
		`SELECT e.id, COALESCE(e.subject, ''), COALESCE(e.from_name, ''),
		        COALESCE(e.from_address, ''), e.received_at,
		        COALESCE(ec.priority_score, 50), COALESCE(LOWER(ec.category), ''), e.account_id,
		        COALESCE(es.headline, LEFT(COALESCE(e.body_text, ''), 120))
		 FROM emails e
		 LEFT JOIN email_categories ec ON ec.email_id = e.id
		 LEFT JOIN email_summaries es ON es.email_id = e.id
		 LEFT JOIN draft_replies dr ON dr.email_id = e.id
		 WHERE e.user_id = $1
		   AND e.direction = 'inbound'
		   AND e.received_at >= $2
		   AND dr.id IS NULL
		   AND COALESCE(ec.priority_score, 50) >= 75
		   AND COALESCE(LOWER(ec.category), '') NOT IN ('promotions','newsletters','spam','updates','shopping','social','events','subscriptions','advertising')
		 ORDER BY COALESCE(ec.priority_score, 50) DESC, e.received_at DESC
		 LIMIT 10`, tc.UserID, weekAgo)
	if attErr == nil {
		defer attRows.Close()
		for attRows.Next() {
			var a attentionEmail
			var receivedAt time.Time
			if attRows.Scan(&a.ID, &a.Subject, &a.FromName, &a.FromAddress,
				&receivedAt, &a.Priority, &a.Category, &a.AccountID, &a.Snippet) == nil {
				a.ReceivedAt = receivedAt.Format(time.RFC3339)
				a.Subject = decodeMIME(a.Subject)
				a.FromName = decodeMIME(a.FromName)
				attention = append(attention, a)
			}
		}
	}
	if attention == nil {
		attention = []attentionEmail{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"emails_today":          emailsToday,
		"emails_total":          emailsTotal,
		"ai_processed":          aiProcessed,
		"pending_replies":       pendingReplies,
		"active_connections":    activeConnections,
		"ai_composed":           draftsToday,
		"avg_response_minutes":  avgResponse,
		"category_distribution": categories,
		"connected_accounts":    connAccounts,
		"requires_attention":    attention,
	})
}

// HandleAuditLog handles GET /api/v1/audit-log.
// Query params: type (filter by task_type), search, page, limit
func (h *DashboardHandlers) HandleAuditLog(w http.ResponseWriter, r *http.Request) {
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

	// Parse query params
	typeFilter := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page := 1
	limit := 50
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := (page - 1) * limit

	// Accept both "draft" and "draft_reply" filter values
	if typeFilter == "draft" {
		typeFilter = "draft_reply"
	}

	type auditEntry struct {
		ID             string  `json:"id"`
		EventType      string  `json:"event_type"`
		EmailSubject   *string `json:"email_subject"`
		EmailSender    *string `json:"email_sender"`
		Description    string  `json:"description"`
		Decision       *string `json:"decision"`
		Reason         *string `json:"reason"`
		ModelUsed      *string `json:"model_used"`
		TokensConsumed *int    `json:"tokens_consumed"`
		LatencyMs      *int    `json:"latency_ms"`
		Confidence     *int    `json:"confidence"`
		CacheHit       bool    `json:"cache_hit"`
		CreatedAt      string  `json:"created_at"`
	}

	// Build query
	query := `SELECT a.id, COALESCE(a.task_type, 'system'), COALESCE(a.model_used, ''),
	                 COALESCE(e.subject, ''), COALESCE(e.from_name, ''), COALESCE(e.from_address, ''),
	                 a.tokens_in, a.tokens_out, a.latency_ms, COALESCE(a.confidence, 0),
	                 COALESCE(a.cache_hit, false),
	                 COALESCE(ec.category, ''), COALESCE(ec.priority_score, 0),
	                 a.created_at
	          FROM ai_audit_log a
	          LEFT JOIN emails e ON e.id = a.email_id
	          LEFT JOIN email_categories ec ON ec.email_id = a.email_id
	          WHERE a.user_id = $1`
	args := []interface{}{tc.UserID}
	argN := 2

	if typeFilter != "" {
		query += ` AND a.task_type = $` + strconv.Itoa(argN)
		args = append(args, typeFilter)
		argN++
	}
	if search != "" {
		query += ` AND (e.subject ILIKE $` + strconv.Itoa(argN) + ` OR e.from_name ILIKE $` + strconv.Itoa(argN) + `)`
		args = append(args, "%"+search+"%")
		argN++
	}

	query += ` ORDER BY a.created_at DESC LIMIT $` + strconv.Itoa(argN) + ` OFFSET $` + strconv.Itoa(argN+1)
	args = append(args, limit, offset)

	rows, err := conn.Query(tc, query, args...)
	if err != nil {
		slog.Error("query audit log", "error", err)
		WriteJSON(w, r, http.StatusOK, []auditEntry{})
		return
	}
	defer rows.Close()

	var entries []auditEntry
	for rows.Next() {
		var e auditEntry
		var model, subject, fromName, fromAddress, category string
		var tokensIn, tokensOut, latencyMs, confidence, priorityScore int
		var cacheHit bool
		var createdAt time.Time

		if err := rows.Scan(&e.ID, &e.EventType, &model,
			&subject, &fromName, &fromAddress,
			&tokensIn, &tokensOut, &latencyMs, &confidence,
			&cacheHit,
			&category, &priorityScore,
			&createdAt); err != nil {
			slog.Error("scan audit entry", "error", err)
			continue
		}

		e.CreatedAt = createdAt.Format(time.RFC3339)
		e.CacheHit = cacheHit

		subject = decodeMIME(subject)
		fromName = decodeMIME(fromName)

		if model != "" {
			e.ModelUsed = &model
		}
		if subject != "" {
			e.EmailSubject = &subject
		}
		sender := fromName
		if sender == "" {
			sender = fromAddress
		}
		if sender != "" {
			e.EmailSender = &sender
		}

		totalTokens := tokensIn + tokensOut
		if totalTokens > 0 {
			e.TokensConsumed = &totalTokens
		}
		if latencyMs > 0 {
			e.LatencyMs = &latencyMs
		}
		if confidence > 0 {
			e.Confidence = &confidence
		}

		// Build informative description and decision based on event type
		switch e.EventType {
		case "categorize":
			e.Description = "Classified email into category"
			if category != "" {
				decision := "Category: " + category
				if priorityScore > 0 {
					decision += " | Priority: " + strconv.Itoa(priorityScore)
				}
				e.Decision = &decision
			}
		case "summarize":
			e.Description = "Generated executive summary"
			if cacheHit {
				reason := "Served from cache"
				e.Reason = &reason
			}
		case "draft_reply":
			e.Description = "Generated AI draft reply"
			if model != "" {
				tier := "Standard"
				if model == "gpt-oss:120b" {
					tier = "Premium"
				}
				reason := tier + " tier draft"
				e.Reason = &reason
			}
		case "behavior_analysis":
			e.Description = "Computed behavior intelligence metrics"
			reason := "Communication, WLB, stress, relationships, productivity"
			e.Reason = &reason
		default:
			e.Description = "AI processing: " + e.EventType
		}

		entries = append(entries, e)
	}

	if entries == nil {
		entries = []auditEntry{}
	}

	WriteJSON(w, r, http.StatusOK, entries)
}

// HandleLatestDigest generates a real digest from today's email data.
// GET /api/v1/briefings/latest-digest
func (h *DashboardHandlers) HandleLatestDigest(w http.ResponseWriter, r *http.Request) {
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

	// Use user's timezone for "today"
	var digestTZ string
	_ = conn.QueryRow(tc, `SELECT COALESCE(u.timezone, 'UTC') FROM users u WHERE u.id = $1`, tc.UserID).Scan(&digestTZ)
	digestLoc, digestLocErr := time.LoadLocation(digestTZ)
	if digestLocErr != nil {
		digestLoc = time.UTC
	}
	nowDigest := time.Now().In(digestLoc)
	today := time.Date(nowDigest.Year(), nowDigest.Month(), nowDigest.Day(), 0, 0, 0, 0, digestLoc).UTC()
	dateStr := nowDigest.Format("Mon, Jan 2, 2006")

	// Count by category today
	type catCount struct {
		Category string
		Count    int
	}
	var categories []catCount
	catRows, _ := conn.Query(tc,
		`SELECT COALESCE(LOWER(ec.category), 'uncategorized'), COUNT(*)
		 FROM emails e LEFT JOIN email_categories ec ON ec.email_id = e.id
		 WHERE e.user_id = $1 AND e.received_at >= $2
		 GROUP BY LOWER(ec.category) ORDER BY COUNT(*) DESC`, tc.UserID, today)
	if catRows != nil {
		defer catRows.Close()
		for catRows.Next() {
			var c catCount
			if catRows.Scan(&c.Category, &c.Count) == nil {
				categories = append(categories, c)
			}
		}
	}

	// Pending drafts
	var pendingDrafts int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM draft_replies WHERE user_id = $1 AND status = 'pending'`,
		tc.UserID).Scan(&pendingDrafts)

	// High priority emails today
	type urgentEmail struct {
		Subject string
	}
	var urgentEmails []urgentEmail
	urgRows, _ := conn.Query(tc,
		`SELECT COALESCE(e.subject, '')
		 FROM emails e JOIN email_categories ec ON ec.email_id = e.id
		 WHERE e.user_id = $1 AND e.received_at >= $2 AND ec.priority_score >= 70
		 ORDER BY ec.priority_score DESC LIMIT 5`, tc.UserID, today)
	if urgRows != nil {
		defer urgRows.Close()
		for urgRows.Next() {
			var u urgentEmail
			if urgRows.Scan(&u.Subject) == nil {
				u.Subject = decodeMIME(u.Subject)
				urgentEmails = append(urgentEmails, u)
			}
		}
	}

	// Build digest text
	digest := "REGENT DIGEST — " + dateStr + "\n"
	digest += "────────────────────────────────\n"

	if len(urgentEmails) > 0 {
		digest += "Urgent (" + strconv.Itoa(len(urgentEmails)) + ")\n"
		for _, u := range urgentEmails {
			digest += "  • " + truncate(u.Subject, 60) + "\n"
		}
		digest += "\n"
	}

	for _, c := range categories {
		if c.Category == "uncategorized" || c.Count == 0 {
			continue
		}
		label := c.Category
		if len(label) > 0 {
			label = string(label[0]-32) + label[1:] // capitalize first letter
		}
		digest += label + " (" + strconv.Itoa(c.Count) + ")\n"
	}

	if pendingDrafts > 0 {
		digest += "\nDrafts Ready: " + strconv.Itoa(pendingDrafts) + " awaiting your approval\n"
	}

	digest += "────────────────────────────────\n"
	digest += "Reply /urgent for critical items only"

	WriteJSON(w, r, http.StatusOK, map[string]string{"content": digest})
}
