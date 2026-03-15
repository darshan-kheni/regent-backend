package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/auth"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// RulesHandlers handles CRUD for user notification rules.
type RulesHandlers struct {
	pool *pgxpool.Pool
}

// NewRulesHandlers creates a new rules handler set.
func NewRulesHandlers(pool *pgxpool.Pool) *RulesHandlers {
	return &RulesHandlers{pool: pool}
}

type createRuleRequest struct {
	RuleType  string          `json:"rule_type"`
	Condition json.RawMessage `json:"condition"`
	Action    string          `json:"action"`
	Priority  int             `json:"priority"`
	Active    *bool           `json:"active"`
}

type ruleResponse struct {
	ID        uuid.UUID       `json:"id"`
	RuleType  string          `json:"rule_type"`
	Condition json.RawMessage `json:"condition"`
	Action    string          `json:"action"`
	Priority  int             `json:"priority"`
	Active    bool            `json:"active"`
}

// ListRules returns all notification rules for the current user.
func (h *RulesHandlers) ListRules(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}
	user := auth.GetUser(r.Context())

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to acquire connection")
		return
	}
	defer conn.Release()

	rows, err := conn.Query(tc,
		`SELECT id, rule_type, condition, action, priority, active
		 FROM user_notification_rules
		 WHERE user_id = $1
		 ORDER BY priority ASC`, user.ID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to query rules")
		return
	}
	defer rows.Close()

	var rules []ruleResponse
	for rows.Next() {
		var rule ruleResponse
		if err := rows.Scan(&rule.ID, &rule.RuleType, &rule.Condition,
			&rule.Action, &rule.Priority, &rule.Active); err != nil {
			continue
		}
		rules = append(rules, rule)
	}

	if rules == nil {
		rules = []ruleResponse{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

// CreateRule creates a new notification rule.
func (h *RulesHandlers) CreateRule(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}
	user := auth.GetUser(r.Context())

	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if !isValidRuleType(req.RuleType) {
		WriteError(w, r, http.StatusBadRequest, "INVALID_RULE_TYPE",
			"rule_type must be one of: vip, sender, keyword, category, time")
		return
	}
	if !isValidAction(req.Action) {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ACTION",
			"action must be one of: critical, high, normal, suppress")
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to acquire connection")
		return
	}
	defer conn.Release()

	var id uuid.UUID
	err = conn.QueryRow(tc,
		`INSERT INTO user_notification_rules (user_id, tenant_id, rule_type, condition, action, priority, active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		user.ID, tc.TenantID, req.RuleType, req.Condition, req.Action, req.Priority, active,
	).Scan(&id)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to create rule")
		return
	}

	WriteJSON(w, r, http.StatusCreated, ruleResponse{
		ID:        id,
		RuleType:  req.RuleType,
		Condition: req.Condition,
		Action:    req.Action,
		Priority:  req.Priority,
		Active:    active,
	})
}

// UpdateRule updates an existing notification rule.
func (h *RulesHandlers) UpdateRule(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}
	user := auth.GetUser(r.Context())

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "invalid rule ID")
		return
	}

	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if req.RuleType != "" && !isValidRuleType(req.RuleType) {
		WriteError(w, r, http.StatusBadRequest, "INVALID_RULE_TYPE",
			"rule_type must be one of: vip, sender, keyword, category, time")
		return
	}
	if req.Action != "" && !isValidAction(req.Action) {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ACTION",
			"action must be one of: critical, high, normal, suppress")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to acquire connection")
		return
	}
	defer conn.Release()

	result, err := conn.Exec(tc,
		`UPDATE user_notification_rules
		 SET rule_type = COALESCE(NULLIF($1, ''), rule_type),
		     condition = COALESCE($2, condition),
		     action = COALESCE(NULLIF($3, ''), action),
		     priority = $4,
		     active = COALESCE($5, active),
		     updated_at = NOW()
		 WHERE id = $6 AND user_id = $7`,
		req.RuleType, req.Condition, req.Action, req.Priority, req.Active, ruleID, user.ID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to update rule")
		return
	}

	if result.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "rule not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

// DeleteRule deletes a notification rule.
func (h *RulesHandlers) DeleteRule(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}
	user := auth.GetUser(r.Context())

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleID"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "invalid rule ID")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to acquire connection")
		return
	}
	defer conn.Release()

	result, err := conn.Exec(r.Context(),
		`DELETE FROM user_notification_rules WHERE id = $1 AND user_id = $2`,
		ruleID, user.ID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DB_ERROR", "failed to delete rule")
		return
	}

	if result.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "rule not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func isValidRuleType(rt string) bool {
	switch rt {
	case "vip", "sender", "keyword", "category", "time":
		return true
	}
	return false
}

func isValidAction(a string) bool {
	switch a {
	case "critical", "high", "normal", "suppress":
		return true
	}
	return false
}
