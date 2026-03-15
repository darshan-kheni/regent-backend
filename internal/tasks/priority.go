package tasks

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

// PriorityScorer scores task priority based on deadline proximity, sender importance, and urgency keywords.
type PriorityScorer struct {
	pool *pgxpool.Pool
}

// NewPriorityScorer creates a new PriorityScorer.
func NewPriorityScorer(pool *pgxpool.Pool) *PriorityScorer {
	return &PriorityScorer{pool: pool}
}

var urgentKeywords = []string{"asap", "immediately", "right away", "urgent", "right now", "as soon as possible"}
var soonKeywords = []string{"soon", "this week", "promptly", "at your earliest", "timely"}

// Score returns a priority level (p0-p3) based on deadline, sender importance, and urgency signals.
func (s *PriorityScorer) Score(ctx database.TenantContext, input PriorityInput) string {
	// Check urgency keywords in deadline text
	lower := strings.ToLower(input.DeadlineText)
	for _, kw := range urgentKeywords {
		if strings.Contains(lower, kw) {
			return PriorityP0
		}
	}

	// Check explicit priority hint
	if strings.ToLower(input.PriorityHint) == "urgent" {
		return PriorityP0
	}

	// Check deadline proximity
	if input.Deadline != nil {
		hoursUntil := time.Until(*input.Deadline).Hours()
		if hoursUntil < 24 {
			return PriorityP0
		}
		if hoursUntil < 72 {
			return PriorityP1
		}
		if hoursUntil < 168 {
			return PriorityP2
		}
	}

	// Check sender importance from contact_relationships
	importance := s.getSenderImportance(ctx, input.UserID, input.SenderEmail)
	if importance > 50 {
		if input.Deadline != nil {
			return PriorityP1
		}
		return PriorityP2
	}

	// Check "soon" keywords
	for _, kw := range soonKeywords {
		if strings.Contains(lower, kw) {
			return PriorityP1
		}
	}

	// Has any deadline → P2
	if input.Deadline != nil {
		return PriorityP2
	}

	return PriorityP3
}

// getSenderImportance queries Phase 8 contact_relationships for interaction_count.
func (s *PriorityScorer) getSenderImportance(ctx database.TenantContext, userID interface{}, senderEmail string) int {
	if s.pool == nil || senderEmail == "" {
		return 0
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		slog.Debug("priority: failed to acquire connection", "error", err)
		return 0
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		slog.Debug("priority: failed to set RLS", "error", err)
		return 0
	}

	var count int
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(interaction_count, 0) FROM contact_relationships
		 WHERE user_id = $1 AND contact_email = $2`,
		userID, senderEmail,
	).Scan(&count)
	if err != nil {
		slog.Debug("priority: sender not found in contacts",
			"sender", senderEmail,
			"error", fmt.Errorf("querying contact: %w", err),
		)
		return 0
	}
	return count
}
