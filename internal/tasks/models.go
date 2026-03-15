package tasks

import (
	"time"

	"github.com/google/uuid"
)

// Status constants
const (
	StatusToDo       = "to_do"
	StatusInProgress = "in_progress"
	StatusWaiting    = "waiting"
	StatusDone       = "done"
	StatusDismissed  = "dismissed"
)

// Priority constants
const (
	PriorityP0 = "p0"
	PriorityP1 = "p1"
	PriorityP2 = "p2"
	PriorityP3 = "p3"
)

// Type constants
const (
	TypeExplicitRequest = "explicit_request"
	TypeImplicitTask    = "implicit_task"
	TypeSelfCommitment  = "self_commitment"
	TypeRecurring       = "recurring"
)

// Task represents a task extracted from an email or created manually.
type Task struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	EmailID           *uuid.UUID `json:"email_id,omitempty"`
	Title             string     `json:"title"`
	Description       string     `json:"description,omitempty"`
	Type              string     `json:"type"`
	Status            string     `json:"status"`
	Priority          string     `json:"priority"`
	Deadline          *time.Time `json:"deadline,omitempty"`
	DeadlineText      string     `json:"deadline_text,omitempty"`
	NeedsConfirmation bool       `json:"needs_confirmation"`
	AssigneeEmail     string     `json:"assignee_email,omitempty"`
	DelegatedTo       string     `json:"delegated_to,omitempty"`
	DelegatedAt       *time.Time `json:"delegated_at,omitempty"`
	Confidence        float64    `json:"confidence"`
	SourceSubject     string     `json:"source_subject,omitempty"`
	SourceSender      string     `json:"source_sender,omitempty"`
	RecurrenceRule    string     `json:"recurrence_rule,omitempty"`
	NextRecurrence    *time.Time `json:"next_recurrence,omitempty"`
	SnoozedUntil      *time.Time `json:"snoozed_until,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	DismissedAt       *time.Time `json:"dismissed_at,omitempty"`
	CalendarEventID   *uuid.UUID `json:"calendar_event_id,omitempty"`
	TimeTrackedMin    int        `json:"time_tracked_min"`
	IsTiming          bool       `json:"is_timing"`
	TimingStartedAt   *time.Time `json:"timing_started_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// ExtractedTask represents a raw task extracted by the AI from an email.
type ExtractedTask struct {
	Description  string  `json:"description"`
	Type         string  `json:"type"`
	DeadlineText string  `json:"deadline_text"`
	Assignee     string  `json:"assignee_email"`
	PriorityHint string  `json:"priority_hint"`
	Confidence   float64 `json:"confidence"`
}

// TaskReminder represents a scheduled reminder for a task.
type TaskReminder struct {
	ID           uuid.UUID  `json:"id"`
	TaskID       uuid.UUID  `json:"task_id"`
	UserID       uuid.UUID  `json:"user_id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	ReminderType string     `json:"reminder_type"`
	ScheduledAt  time.Time  `json:"scheduled_at"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TaskDelegation represents a delegation of a task to an external contact.
type TaskDelegation struct {
	ID                uuid.UUID  `json:"id"`
	TaskID            uuid.UUID  `json:"task_id"`
	UserID            uuid.UUID  `json:"user_id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	DelegatedToEmail  string     `json:"delegated_to_email"`
	DelegatedToName   string     `json:"delegated_to_name,omitempty"`
	DelegationEmailID *uuid.UUID `json:"delegation_email_id,omitempty"`
	Status            string     `json:"status"`
	FollowUpDate      *time.Time `json:"follow_up_date,omitempty"`
	FollowUpCount     int        `json:"follow_up_count"`
	LastFollowUp      *time.Time `json:"last_follow_up,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// TaskDigest is the content for a daily task digest.
type TaskDigest struct {
	OverdueCount     int    `json:"overdue_count"`
	OverdueTasks     []Task `json:"overdue_tasks"`
	DueTodayCount    int    `json:"due_today_count"`
	DueTodayTasks    []Task `json:"due_today_tasks"`
	DueThisWeekCount int    `json:"due_this_week_count"`
	DueThisWeekTasks []Task `json:"due_this_week_tasks"`
	NewCount         int    `json:"new_count"`
	DelegatedWaiting int    `json:"delegated_waiting"`
}

// TaskStats holds weekly completion statistics.
type TaskStats struct {
	CompletedThisWeek  int     `json:"completed_this_week"`
	CreatedThisWeek    int     `json:"created_this_week"`
	AvgCompletionHours float64 `json:"avg_completion_hours"`
	OnTimeRate         float64 `json:"on_time_rate"`
	OverdueCarried     int     `json:"overdue_carried"`
	TimeTrackedMin     int     `json:"time_tracked_min"`
}

// PriorityInput holds the inputs for priority scoring.
type PriorityInput struct {
	DeadlineText string
	Deadline     *time.Time
	SenderEmail  string
	PriorityHint string
	UserID       uuid.UUID
}

// CompletionResult from delegation completion detection.
type CompletionResult struct {
	Completed   bool    `json:"completed"`
	InProgress  bool    `json:"in_progress"`
	Confidence  float64 `json:"confidence"`
	NeedsReview bool    `json:"needs_review"`
}

// BoardColumn represents a user-customizable Kanban column.
type BoardColumn struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	ColumnKey string    `json:"column_key"`
	Label     string    `json:"label"`
	Color     string    `json:"color"`
	Position  int       `json:"position"`
	IsDefault bool      `json:"is_default"`
}

// TimeEntry represents a time tracking entry for a task.
type TimeEntry struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      uuid.UUID  `json:"task_id"`
	UserID      uuid.UUID  `json:"user_id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	DurationMin *int       `json:"duration_min,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// DefaultColumns returns the default 4-column Kanban layout.
func DefaultColumns() []BoardColumn {
	return []BoardColumn{
		{ColumnKey: "to_do", Label: "To Do", Color: "#3B82F6", Position: 0, IsDefault: true},
		{ColumnKey: "in_progress", Label: "In Progress", Color: "#C9A96E", Position: 1, IsDefault: true},
		{ColumnKey: "waiting", Label: "Waiting", Color: "#6B7280", Position: 2, IsDefault: true},
		{ColumnKey: "done", Label: "Done", Color: "#22C55E", Position: 3, IsDefault: true},
	}
}

// DismissedFeedback records a dismissed task for the AI feedback loop.
type DismissedFeedback struct {
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	TaskTitle       string    `json:"task_title"`
	TaskDescription string    `json:"task_description,omitempty"`
	SourceSender    string    `json:"source_sender,omitempty"`
	SourceSubject   string    `json:"source_subject,omitempty"`
	TaskType        string    `json:"task_type,omitempty"`
	DismissedAt     time.Time `json:"dismissed_at"`
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
