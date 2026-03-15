package calendar

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SlotApproval handles the approval of a suggested slot.
type SlotApproval struct {
	pool       *pgxpool.Pool
	syncEngine *SyncEngine
}

// NewSlotApproval creates a SlotApproval with the given pool and optional sync engine.
func NewSlotApproval(pool *pgxpool.Pool, syncEngine *SyncEngine) *SlotApproval {
	return &SlotApproval{pool: pool, syncEngine: syncEngine}
}

// ApproveSlot creates a calendar event for the approved slot and updates the scheduling request.
func (sa *SlotApproval) ApproveSlot(ctx database.TenantContext, requestID uuid.UUID, slotIndex int) error {
	// Load the scheduling request
	conn, err := sa.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	var req SchedulingRequest
	err = conn.QueryRow(ctx,
		`SELECT id, user_id, tenant_id, email_id, suggested_slots, status
		FROM scheduling_requests WHERE id = $1 AND user_id = $2`,
		requestID, ctx.UserID,
	).Scan(&req.ID, &req.UserID, &req.TenantID, &req.EmailID, &req.SuggestedSlots, &req.Status)
	if err != nil {
		return fmt.Errorf("scheduling request not found: %w", err)
	}

	if req.Status != "suggested" {
		return fmt.Errorf("request status is %s, expected 'suggested'", req.Status)
	}

	// Parse suggested slots
	var slots []SuggestedSlot
	if err := json.Unmarshal(req.SuggestedSlots, &slots); err != nil {
		return fmt.Errorf("parsing suggested slots: %w", err)
	}

	if slotIndex < 0 || slotIndex >= len(slots) {
		return fmt.Errorf("slot index %d out of range (0-%d)", slotIndex, len(slots)-1)
	}

	slot := slots[slotIndex]

	// Check for new conflicts since suggestion
	checker := NewConflictChecker(sa.pool)
	events, err := checker.getEventsInRange(ctx, slot.Start, slot.End)
	if err != nil {
		return fmt.Errorf("checking conflicts: %w", err)
	}
	prefs, _ := checker.getPreferences(ctx)
	conflicts := DetectConflicts(events, prefs)
	for _, c := range conflicts {
		if c.Type == "hard" {
			return fmt.Errorf("new hard conflict detected since suggestion")
		}
	}

	// Update request status to accepted
	acceptedJSON, _ := json.Marshal(slot)
	_, err = conn.Exec(ctx,
		`UPDATE scheduling_requests
		SET status = 'accepted', accepted_slot = $1, updated_at = now()
		WHERE id = $2 AND user_id = $3`,
		acceptedJSON, requestID, ctx.UserID,
	)
	if err != nil {
		return fmt.Errorf("updating request status: %w", err)
	}

	slog.Info("slot approved",
		"request_id", requestID,
		"slot_start", slot.Start,
		"slot_end", slot.End,
	)

	// TODO: Create event via provider API (google.go/microsoft.go CreateEvent)
	// TODO: Generate and send confirmation email reply

	return nil
}
