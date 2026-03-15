package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
	"github.com/darshan-kheni/regent/internal/tasks"
)

// TaskHandlers handles task-related HTTP endpoints.
type TaskHandlers struct {
	pool      *pgxpool.Pool
	rdb       *redis.Client
	extractor *tasks.TaskExtractor
	delegator *tasks.DelegationService
}

// NewTaskHandlers creates new task handlers.
func NewTaskHandlers(pool *pgxpool.Pool, rdb *redis.Client) *TaskHandlers {
	return &TaskHandlers{
		pool:      pool,
		rdb:       rdb,
		extractor: tasks.NewTaskExtractor(pool),
		delegator: tasks.NewDelegationService(pool),
	}
}

// HandleListTasks returns paginated tasks with filters.
// GET /tasks?status=to_do,in_progress&priority=p0,p1&type=explicit_request&search=text
func (h *TaskHandlers) HandleListTasks(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	// Build query with filters — COALESCE nullable text/int columns to prevent scan errors
	query := `SELECT id, user_id, tenant_id, email_id, title,
	                 COALESCE(description, ''), COALESCE(type, 'explicit_request'),
	                 COALESCE(status, 'to_do'), COALESCE(priority, 'p2'),
	                 deadline, COALESCE(deadline_text, ''),
	                 COALESCE(needs_confirmation, false), COALESCE(assignee_email, ''),
	                 COALESCE(delegated_to, ''), delegated_at,
	                 COALESCE(confidence, 0), COALESCE(source_subject, ''), COALESCE(source_sender, ''),
	                 COALESCE(recurrence_rule, ''), next_recurrence, snoozed_until,
	                 completed_at, dismissed_at, calendar_event_id,
	                 COALESCE(time_tracked_min, 0), COALESCE(is_timing, false), timing_started_at,
	                 COALESCE(created_at, now())
	          FROM tasks WHERE user_id = $1`
	args := []interface{}{tc.UserID}
	argIdx := 2

	// Status filter
	if statuses := r.URL.Query().Get("status"); statuses != "" {
		query += ` AND status = ANY($` + strconv.Itoa(argIdx) + `::text[])`
		args = append(args, splitCSVParam(statuses))
		argIdx++
	} else {
		// Default: exclude dismissed
		query += ` AND status != 'dismissed'`
	}

	// Priority filter
	if priorities := r.URL.Query().Get("priority"); priorities != "" {
		query += ` AND priority = ANY($` + strconv.Itoa(argIdx) + `::text[])`
		args = append(args, splitCSVParam(priorities))
		argIdx++
	}

	// Type filter
	if types := r.URL.Query().Get("type"); types != "" {
		query += ` AND type = ANY($` + strconv.Itoa(argIdx) + `::text[])`
		args = append(args, splitCSVParam(types))
		argIdx++
	}

	// Search filter
	if search := r.URL.Query().Get("search"); search != "" {
		query += ` AND (title ILIKE $` + strconv.Itoa(argIdx) + ` OR description ILIKE $` + strconv.Itoa(argIdx) + `)`
		args = append(args, "%"+search+"%")
		argIdx++
	}

	query += ` ORDER BY
		CASE priority WHEN 'p0' THEN 0 WHEN 'p1' THEN 1 WHEN 'p2' THEN 2 ELSE 3 END,
		COALESCE(deadline, '9999-12-31'::timestamptz),
		created_at DESC
	  LIMIT 200`

	rows, err := conn.Query(r.Context(), query, args...)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to fetch tasks")
		return
	}
	defer rows.Close()

	var taskList []tasks.Task
	for rows.Next() {
		var t tasks.Task
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.TenantID, &t.EmailID,
			&t.Title, &t.Description, &t.Type, &t.Status,
			&t.Priority, &t.Deadline, &t.DeadlineText, &t.NeedsConfirmation,
			&t.AssigneeEmail, &t.DelegatedTo, &t.DelegatedAt,
			&t.Confidence, &t.SourceSubject, &t.SourceSender,
			&t.RecurrenceRule, &t.NextRecurrence, &t.SnoozedUntil,
			&t.CompletedAt, &t.DismissedAt, &t.CalendarEventID,
			&t.TimeTrackedMin, &t.IsTiming, &t.TimingStartedAt,
			&t.CreatedAt,
		); err != nil {
			WriteError(w, r, http.StatusInternalServerError, "SCAN_ERROR", "Failed to parse task")
			return
		}
		taskList = append(taskList, t)
	}

	if taskList == nil {
		taskList = []tasks.Task{}
	}

	WriteJSON(w, r, http.StatusOK, taskList)
}

// HandleCreateTask creates a manual task (quick-add).
// POST /tasks
func (h *TaskHandlers) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Deadline    string `json:"deadline"`
		Type        string `json:"type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	if input.Title == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_TITLE", "Title is required")
		return
	}

	if input.Priority == "" {
		input.Priority = tasks.PriorityP2
	}
	if input.Type == "" {
		input.Type = tasks.TypeExplicitRequest
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	// Parse deadline if provided
	var deadline *time.Time
	if input.Deadline != "" {
		parser := tasks.NewDeadlineParser()
		deadline = parser.Parse(input.Deadline, time.Now())
	}

	var task tasks.Task
	err = conn.QueryRow(r.Context(),
		`INSERT INTO tasks (user_id, tenant_id, title, description, type, status, priority, deadline, confidence)
		 VALUES ($1, $2, $3, $4, $5, 'to_do', $6, $7, 1.0)
		 RETURNING id, created_at`,
		tc.UserID, tc.TenantID, input.Title, input.Description, input.Type, input.Priority, deadline,
	).Scan(&task.ID, &task.CreatedAt)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INSERT_ERROR", "Failed to create task")
		return
	}

	task.UserID = tc.UserID
	task.TenantID = tc.TenantID
	task.Title = input.Title
	task.Description = input.Description
	task.Type = input.Type
	task.Status = tasks.StatusToDo
	task.Priority = input.Priority
	task.Deadline = deadline
	task.Confidence = 1.0

	WriteJSON(w, r, http.StatusCreated, task)
}

// HandleUpdateTask updates task fields.
// PATCH /tasks/{id}
func (h *TaskHandlers) HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid task ID")
		return
	}

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	// Build dynamic UPDATE from input fields
	setClauses := ""
	args := []interface{}{taskID, tc.UserID}
	argIdx := 3

	allowedFields := map[string]bool{
		"title": true, "description": true, "priority": true,
		"deadline": true, "type": true, "needs_confirmation": true,
	}

	for field, value := range input {
		if !allowedFields[field] {
			continue
		}
		if setClauses != "" {
			setClauses += ", "
		}
		setClauses += field + " = $" + strconv.Itoa(argIdx)
		args = append(args, value)
		argIdx++
	}

	if setClauses == "" {
		WriteError(w, r, http.StatusBadRequest, "NO_FIELDS", "No valid fields to update")
		return
	}

	_, err = conn.Exec(r.Context(),
		`UPDATE tasks SET `+setClauses+` WHERE id = $1 AND user_id = $2`,
		args...,
	)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "UPDATE_ERROR", "Failed to update task")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleUpdateStatus changes task status (drag-drop).
// PATCH /tasks/{id}/status
func (h *TaskHandlers) HandleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid task ID")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	// Set completed_at if moving to done
	var completedAt *time.Time
	if input.Status == tasks.StatusDone {
		now := time.Now()
		completedAt = &now
	}

	_, err = conn.Exec(r.Context(),
		`UPDATE tasks SET status = $1, completed_at = $2 WHERE id = $3 AND user_id = $4`,
		input.Status, completedAt, taskID, tc.UserID,
	)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "UPDATE_ERROR", "Failed to update status")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "updated"})
}

// HandleDismissTask dismisses a task.
// DELETE /tasks/{id}
func (h *TaskHandlers) HandleDismissTask(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid task ID")
		return
	}

	if err := h.extractor.DismissTask(tc, taskID); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DISMISS_ERROR", "Failed to dismiss task")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "dismissed"})
}

// HandleSnooze snoozes a task until a given date.
// POST /tasks/{id}/snooze
func (h *TaskHandlers) HandleSnooze(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid task ID")
		return
	}

	var input struct {
		Until time.Time `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	_, err = conn.Exec(r.Context(),
		`UPDATE tasks SET snoozed_until = $1 WHERE id = $2 AND user_id = $3`,
		input.Until, taskID, tc.UserID,
	)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "SNOOZE_ERROR", "Failed to snooze task")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "snoozed"})
}

// HandleDelegate delegates a task to an external contact.
// POST /tasks/{id}/delegate
func (h *TaskHandlers) HandleDelegate(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid task ID")
		return
	}

	var input struct {
		Email      string    `json:"email"`
		Name       string    `json:"name"`
		FollowUpAt time.Time `json:"follow_up_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	if input.Email == "" {
		WriteError(w, r, http.StatusBadRequest, "MISSING_EMAIL", "Delegatee email is required")
		return
	}

	delegation, err := h.delegator.Delegate(tc, tasks.DelegateInput{
		TaskID:         taskID,
		DelegateeEmail: input.Email,
		DelegateeName:  input.Name,
		FollowUpDate:   input.FollowUpAt,
	})
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DELEGATE_ERROR", "Failed to delegate task")
		return
	}

	WriteJSON(w, r, http.StatusCreated, map[string]interface{}{"data": delegation})
}

// HandleGetDelegations returns delegation history for a task.
// GET /tasks/{id}/delegations
func (h *TaskHandlers) HandleGetDelegations(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	taskID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_ID", "Invalid task ID")
		return
	}

	delegations, err := h.delegator.GetDelegations(tc, taskID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "QUERY_ERROR", "Failed to fetch delegations")
		return
	}

	if delegations == nil {
		delegations = []tasks.TaskDelegation{}
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": delegations})
}

// HandleGetDigest returns daily digest content.
// GET /tasks/digest
func (h *TaskHandlers) HandleGetDigest(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	var digest tasks.TaskDigest

	// Overdue
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND deadline < now() AND status NOT IN ('done','dismissed')`,
		tc.UserID,
	).Scan(&digest.OverdueCount)

	// Due today
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND deadline >= CURRENT_DATE AND deadline < CURRENT_DATE + interval '1 day' AND status NOT IN ('done','dismissed')`,
		tc.UserID,
	).Scan(&digest.DueTodayCount)

	// Due this week
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND deadline >= CURRENT_DATE AND deadline < CURRENT_DATE + interval '7 days' AND status NOT IN ('done','dismissed')`,
		tc.UserID,
	).Scan(&digest.DueThisWeekCount)

	// New (created in last 24h)
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND created_at > now() - interval '24 hours'`,
		tc.UserID,
	).Scan(&digest.NewCount)

	// Delegated waiting
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status = 'waiting'`,
		tc.UserID,
	).Scan(&digest.DelegatedWaiting)

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": digest})
}

// HandleGetStats returns weekly completion statistics.
// GET /tasks/stats
func (h *TaskHandlers) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "DATABASE_ERROR", "Failed to acquire connection")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context")
		return
	}

	var stats tasks.TaskStats

	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status = 'done' AND completed_at > now() - interval '7 days'`,
		tc.UserID,
	).Scan(&stats.CompletedThisWeek)

	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND created_at > now() - interval '7 days'`,
		tc.UserID,
	).Scan(&stats.CreatedThisWeek)

	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND deadline < now() AND status NOT IN ('done','dismissed')`,
		tc.UserID,
	).Scan(&stats.OverdueCarried)

	conn.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(time_tracked_min), 0) FROM tasks WHERE user_id = $1 AND created_at > now() - interval '7 days'`,
		tc.UserID,
	).Scan(&stats.TimeTrackedMin)

	// On-time rate: completed tasks that were done before deadline
	var onTime, withDeadline int
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status = 'done' AND deadline IS NOT NULL AND completed_at > now() - interval '7 days'`,
		tc.UserID,
	).Scan(&withDeadline)
	conn.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status = 'done' AND deadline IS NOT NULL AND completed_at <= deadline AND completed_at > now() - interval '7 days'`,
		tc.UserID,
	).Scan(&onTime)
	if withDeadline > 0 {
		stats.OnTimeRate = float64(onTime) / float64(withDeadline)
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{"data": stats})
}

// splitCSVParam splits a comma-separated query parameter into a string slice.
func splitCSVParam(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
