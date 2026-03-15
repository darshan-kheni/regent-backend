package calendar

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BriefEngine generates meeting prep briefs.
type BriefEngine struct {
	pool *pgxpool.Pool
	cfg  *config.CalendarConfig
}

// NewBriefEngine creates a BriefEngine.
func NewBriefEngine(pool *pgxpool.Pool, cfg *config.CalendarConfig) *BriefEngine {
	return &BriefEngine{pool: pool, cfg: cfg}
}

// CheckAndGenerateBriefs finds upcoming unbriefed meetings and generates prep briefs.
func (b *BriefEngine) CheckAndGenerateBriefs(ctx database.TenantContext) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	leadMinutes := b.cfg.BriefLeadMinutes + 1
	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, title, description, start_time, end_time,
			attendees, meeting_url, location
		FROM calendar_events
		WHERE user_id = $1
		  AND start_time BETWEEN now() AND now() + ($2 * interval '1 minute')
		  AND briefed_at IS NULL
		  AND status != 'cancelled'`,
		ctx.UserID, leadMinutes)
	if err != nil {
		return fmt.Errorf("querying unbriefed events: %w", err)
	}
	defer rows.Close()

	var events []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.TenantID, &e.Title, &e.Description,
			&e.StartTime, &e.EndTime, &e.Attendees, &e.MeetingURL, &e.Location); err != nil {
			slog.Error("scan unbriefed event", "err", err)
			continue
		}
		events = append(events, e)
	}

	for _, event := range events {
		if err := b.generateBrief(ctx, event); err != nil {
			slog.Error("generate brief failed", "event_id", event.ID, "err", err)
		}
	}

	return nil
}

func (b *BriefEngine) generateBrief(ctx database.TenantContext, event CalendarEvent) error {
	// Gather attendee context
	attendeeCtx := b.gatherAttendeeContext(ctx, event)

	// Detect agenda from email threads
	agenda := b.detectAgenda(ctx, event)

	// Build brief text (placeholder — actual AI call would go here)
	briefText := b.buildBriefText(event, attendeeCtx, agenda)

	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	// Store brief
	attendeeJSON, _ := json.Marshal(attendeeCtx)
	_, err = conn.Exec(ctx,
		`INSERT INTO meeting_briefs (event_id, user_id, tenant_id, brief_text, model_used, tokens_used, attendee_context, agenda_detected)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (event_id, user_id) DO NOTHING`,
		event.ID, ctx.UserID, ctx.TenantID, briefText, "ministral-3:8b", 400, attendeeJSON, agenda)
	if err != nil {
		return fmt.Errorf("storing brief: %w", err)
	}

	// Mark event as briefed
	_, err = conn.Exec(ctx,
		`UPDATE calendar_events SET briefed_at = now() WHERE id = $1`,
		event.ID)
	if err != nil {
		return fmt.Errorf("marking briefed: %w", err)
	}

	slog.Info("meeting brief generated",
		"event_id", event.ID,
		"title", event.Title,
		"start_time", event.StartTime,
	)

	// TODO: Deliver via Phase 5 briefings.PublishNotificationEvent()
	return nil
}

func (b *BriefEngine) gatherAttendeeContext(ctx database.TenantContext, event CalendarEvent) []AttendeeContext {
	var attendeeEmails []struct {
		Email string `json:"email"`
		Name  string `json:"displayName"`
	}
	if event.Attendees != nil {
		_ = json.Unmarshal(event.Attendees, &attendeeEmails)
	}

	var contexts []AttendeeContext
	for _, a := range attendeeEmails {
		ac := AttendeeContext{
			Email: a.Email,
			Name:  a.Name,
		}
		// Load from contact_relationships if available
		b.enrichFromContacts(ctx, &ac)
		contexts = append(contexts, ac)
	}
	return contexts
}

func (b *BriefEngine) enrichFromContacts(ctx database.TenantContext, ac *AttendeeContext) {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return
	}

	var name, company, dominantTone string
	var interactionCount int
	var lastInteraction *time.Time
	err = conn.QueryRow(ctx,
		`SELECT contact_name, company, interaction_count, dominant_tone, last_interaction
		FROM contact_relationships WHERE user_id = $1 AND contact_email = $2`,
		ctx.UserID, ac.Email,
	).Scan(&name, &company, &interactionCount, &dominantTone, &lastInteraction)
	if err != nil {
		return // No contact data
	}
	if name != "" {
		ac.Name = name
	}
	ac.Company = company
	ac.InteractionCount = interactionCount
	ac.DominantTone = dominantTone
	if lastInteraction != nil {
		ac.LastInteraction = lastInteraction.Format("2006-01-02")
	}
}

var agendaPattern = regexp.MustCompile(`(?i)(?:agenda|topics|discuss):?\s*(?:\n\s*[-\x{2022}*\d]+\s*.+)+`)

func (b *BriefEngine) detectAgenda(ctx database.TenantContext, event CalendarEvent) string {
	// Search recent emails with meeting subject or attendees for agenda items
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return ""
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return ""
	}

	var body string
	err = conn.QueryRow(ctx,
		`SELECT body FROM emails WHERE user_id = $1 AND subject ILIKE '%' || $2 || '%'
		ORDER BY received_at DESC LIMIT 1`,
		ctx.UserID, event.Title,
	).Scan(&body)
	if err != nil || body == "" {
		return ""
	}

	if match := agendaPattern.FindString(body); match != "" {
		return match
	}
	return ""
}

func (b *BriefEngine) buildBriefText(event CalendarEvent, attendees []AttendeeContext, agenda string) string {
	// Placeholder: would call ministral-3:8b AI in production
	text := fmt.Sprintf("Meeting: %s\n", event.Title)
	for _, a := range attendees {
		text += fmt.Sprintf("- %s", a.Name)
		if a.Company != "" {
			text += fmt.Sprintf(" (%s)", a.Company)
		}
		if a.InteractionCount > 0 {
			text += fmt.Sprintf(" -- %d prior interactions", a.InteractionCount)
		}
		text += "\n"
	}
	if agenda != "" {
		text += fmt.Sprintf("\nAgenda:\n%s\n", agenda)
	}
	return text
}
