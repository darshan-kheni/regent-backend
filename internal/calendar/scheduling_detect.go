package calendar

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SchedulingDetector processes scheduling analysis results from AI summarization.
type SchedulingDetector struct {
	pool *pgxpool.Pool
}

// NewSchedulingDetector creates a new SchedulingDetector.
func NewSchedulingDetector(pool *pgxpool.Pool) *SchedulingDetector {
	return &SchedulingDetector{pool: pool}
}

// ProcessAnalysis stores a detected scheduling request from AI summarization output.
// Called after each email summarization when SchedulingAnalysis is non-nil.
func (sd *SchedulingDetector) ProcessAnalysis(ctx database.TenantContext, emailID uuid.UUID, analysis *SchedulingAnalysis, emailReceivedAt time.Time) error {
	if analysis == nil || !analysis.HasIntent || analysis.Confidence < 0.5 {
		return nil
	}

	// Parse proposed times through 3-layer parser
	var parsedTimes []ProposedTime
	for _, pt := range analysis.ProposedTimes {
		parsed := pt
		if result, err := ParseSchedulingTime(pt.Text, emailReceivedAt); err == nil {
			parsed.Parsed = result.Start.Format(time.RFC3339)
		}
		parsedTimes = append(parsedTimes, parsed)
	}

	proposedJSON, _ := json.Marshal(parsedTimes)
	attendeesJSON, _ := json.Marshal(analysis.Attendees)

	conn, err := sd.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO scheduling_requests (
			user_id, tenant_id, email_id, confidence, proposed_times, duration_hint,
			attendees, location_preference, urgency, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'detected')
		ON CONFLICT DO NOTHING`,
		ctx.UserID, ctx.TenantID, emailID, analysis.Confidence,
		proposedJSON, analysis.DurationHint,
		attendeesJSON, nullIfEmpty(analysis.LocationPreference),
		nullIfEmpty(analysis.Urgency),
	)
	if err != nil {
		slog.Error("failed to store scheduling request", "email_id", emailID, "err", err)
		return err
	}

	slog.Info("scheduling request detected",
		"user_id", ctx.UserID,
		"email_id", emailID,
		"confidence", analysis.Confidence,
	)

	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
