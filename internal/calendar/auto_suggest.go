package calendar

import (
	"encoding/json"
	"log/slog"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AutoSuggest handles automatic slot suggestion for high-confidence scheduling requests.
type AutoSuggest struct {
	pool       *pgxpool.Pool
	slotEngine *SlotEngine
}

// NewAutoSuggest creates a new AutoSuggest with the given pool and slot engine.
func NewAutoSuggest(pool *pgxpool.Pool, slotEngine *SlotEngine) *AutoSuggest {
	return &AutoSuggest{pool: pool, slotEngine: slotEngine}
}

// ProcessHighConfidence checks if a scheduling request warrants automatic slot suggestions.
// confidence > 0.8 → find slots and update to "suggested"
// confidence 0.5-0.8 → stays "detected" for manual sidebar action
func (as *AutoSuggest) ProcessHighConfidence(ctx database.TenantContext, requestID uuid.UUID, analysis *SchedulingAnalysis) error {
	if analysis == nil || analysis.Confidence <= 0.8 {
		return nil // Below threshold for auto-suggest
	}

	if as.slotEngine == nil {
		slog.Debug("slot engine not available, skipping auto-suggest")
		return nil
	}

	duration := analysis.DurationHint
	if duration == 0 {
		duration = 60 // Default 1 hour
	}

	slots, err := as.slotEngine.SuggestSlots(ctx, SlotRequest{
		Attendees:       analysis.Attendees,
		DurationMinutes: duration,
		LocationPref:    analysis.LocationPreference,
	})
	if err != nil {
		slog.Error("auto-suggest failed", "request_id", requestID, "err", err)
		return nil // Non-fatal: request stays as "detected"
	}

	if len(slots) == 0 {
		return nil
	}

	slotsJSON, _ := json.Marshal(slots)

	conn, err := as.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`UPDATE scheduling_requests
		SET suggested_slots = $1, status = 'suggested', updated_at = now()
		WHERE id = $2 AND user_id = $3`,
		slotsJSON, requestID, ctx.UserID,
	)

	if err != nil {
		slog.Error("failed to update scheduling request with suggestions", "err", err)
		return err
	}

	slog.Info("auto-suggested slots for scheduling request",
		"request_id", requestID,
		"slots", len(slots),
	)

	return nil
}
