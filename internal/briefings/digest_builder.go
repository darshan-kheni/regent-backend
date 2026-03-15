package briefings

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DigestBuilder queries emails since the last digest and groups them by priority tier.
type DigestBuilder struct {
	pool *pgxpool.Pool
}

// NewDigestBuilder creates a digest builder.
func NewDigestBuilder(pool *pgxpool.Pool) *DigestBuilder {
	return &DigestBuilder{pool: pool}
}

// DigestData holds the compiled digest content grouped by priority tier.
type DigestData struct {
	UserID          uuid.UUID
	TenantID        uuid.UUID
	PeriodStart     time.Time
	PeriodEnd       time.Time
	Urgent          []DigestEmail // Priority > 80 (red section)
	NeedsReply      []DigestEmail // Has draft reply (gold section)
	FYI             []DigestEmail // Everything else (gray section)
	TotalCount      int
	UrgentCount     int
	NeedsReplyCount int
	TimeSaved       string // Estimated time saved by AI
}

// DigestEmail represents a single email entry in a digest.
type DigestEmail struct {
	ID              uuid.UUID
	SenderName      string
	SenderAddress   string
	Subject         string
	Summary         string
	Category        string
	Priority        int
	HasDraftReply   bool
	SuggestedAction string
	ReceivedAt      time.Time
	ThreadCount     int // Number of messages in thread
}

// Build queries emails since lastDigest and groups them into priority tiers.
func (b *DigestBuilder) Build(ctx context.Context, userID, tenantID uuid.UUID, lastDigest time.Time) (*DigestData, error) {
	if b.pool == nil {
		return &DigestData{
			UserID:      userID,
			TenantID:    tenantID,
			PeriodStart: lastDigest,
			PeriodEnd:   time.Now(),
		}, nil
	}

	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("digest builder: acquire connection: %w", err)
	}
	defer conn.Release()

	// Set tenant context for RLS
	_, err = conn.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID)
	if err != nil {
		return nil, fmt.Errorf("digest builder: set tenant context: %w", err)
	}

	now := time.Now()
	data := &DigestData{
		UserID:      userID,
		TenantID:    tenantID,
		PeriodStart: lastDigest,
		PeriodEnd:   now,
	}

	rows, err := conn.Query(ctx,
		`SELECT e.id, COALESCE(e.from_name, ''), e.from_address, COALESCE(e.subject, ''), e.received_at,
		        COALESCE(es.summary, '') as summary,
		        COALESCE(ec.category, 'General') as category,
		        COALESCE(ec.confidence, 0) as priority,
		        EXISTS(SELECT 1 FROM draft_replies dr WHERE dr.email_id = e.id) as has_draft
		 FROM emails e
		 LEFT JOIN email_summaries es ON es.email_id = e.id
		 LEFT JOIN email_categories ec ON ec.email_id = e.id
		 WHERE e.user_id = $1 AND e.received_at > $2
		 ORDER BY ec.confidence DESC NULLS LAST, e.received_at DESC`,
		userID, lastDigest,
	)
	if err != nil {
		return nil, fmt.Errorf("digest builder: query emails: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var de DigestEmail
		var priority float64
		if err := rows.Scan(
			&de.ID, &de.SenderName, &de.SenderAddress, &de.Subject, &de.ReceivedAt,
			&de.Summary, &de.Category, &priority, &de.HasDraftReply,
		); err != nil {
			continue
		}
		de.Priority = int(priority * 100)
		data.TotalCount++

		switch {
		case de.Priority > 80:
			data.Urgent = append(data.Urgent, de)
			data.UrgentCount++
		case de.HasDraftReply:
			data.NeedsReply = append(data.NeedsReply, de)
			data.NeedsReplyCount++
		default:
			data.FYI = append(data.FYI, de)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("digest builder: iterate rows: %w", err)
	}

	return data, nil
}
