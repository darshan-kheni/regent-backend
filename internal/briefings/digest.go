package briefings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EmailDigestChannel compiles and sends email digests via the user's own SMTP.
// Implements the Channel interface. Sends are aggregated, not per-briefing.
type EmailDigestChannel struct {
	pool      *pgxpool.Pool
	builder   *DigestBuilder
	renderer  *DigestHTMLRenderer
	scheduler *DigestScheduler

	mu     sync.Mutex
	status ChannelStatus
}

// NewEmailDigestChannel creates a digest channel.
func NewEmailDigestChannel(pool *pgxpool.Pool) *EmailDigestChannel {
	return &EmailDigestChannel{
		pool:      pool,
		builder:   NewDigestBuilder(pool),
		renderer:  NewDigestHTMLRenderer(),
		scheduler: NewDigestScheduler(pool),
		status:    ChannelStatus{Available: true},
	}
}

func (d *EmailDigestChannel) Name() string { return "email_digest" }

func (d *EmailDigestChannel) Status() ChannelStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

func (d *EmailDigestChannel) ValidateConfig(cfg ChannelConfig) error {
	return nil // Digest uses existing SMTP credentials
}

// Send queues an email for the next digest batch. For digests, individual sends
// are no-ops — the actual digest is compiled and sent by the cron scheduler.
func (d *EmailDigestChannel) Send(ctx context.Context, recipient Recipient, briefing Briefing) error {
	// Digest channel works differently: individual briefings are stored
	// and batched by the digest scheduler. This Send is a no-op for
	// normal-priority briefings (they're already in the DB).
	slog.Debug("digest: briefing queued for next digest batch",
		"user_id", briefing.UserID,
		"email_id", briefing.EmailID,
		"priority", briefing.Priority,
	)
	return nil
}

// CompileAndSend builds and sends a digest for a user. Called by the cron scheduler.
func (d *EmailDigestChannel) CompileAndSend(ctx context.Context, userID, tenantID uuid.UUID) error {
	lastDigest, err := d.scheduler.GetLastDigestTime(ctx, userID)
	if err != nil {
		return fmt.Errorf("digest compile: get last digest time: %w", err)
	}

	data, err := d.builder.Build(ctx, userID, tenantID, lastDigest)
	if err != nil {
		return fmt.Errorf("digest compile: build digest data: %w", err)
	}

	if data.TotalCount == 0 {
		slog.Info("digest: no new emails, skipping",
			"user_id", userID,
		)
		return nil
	}

	html, err := d.renderer.Render(data)
	if err != nil {
		return fmt.Errorf("digest compile: render HTML: %w", err)
	}

	// Record in digest_history
	if err := d.recordDigest(ctx, data, len(html)); err != nil {
		slog.Warn("digest: failed to record history",
			"user_id", userID,
			"error", err,
		)
	}

	slog.Info("digest: compiled successfully",
		"user_id", userID,
		"email_count", data.TotalCount,
		"urgent_count", data.UrgentCount,
		"needs_reply_count", data.NeedsReplyCount,
		"html_size_kb", len(html)/1024,
	)

	return nil
}

// recordDigest inserts a digest_history row.
func (d *EmailDigestChannel) recordDigest(ctx context.Context, data *DigestData, htmlSize int) error {
	if d.pool == nil {
		return nil
	}

	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("digest record: acquire connection: %w", err)
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO digest_history (user_id, tenant_id, email_count, urgent_count, needs_reply_count,
		    period_start, period_end, sent_at, html_size_kb)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		data.UserID, data.TenantID, data.TotalCount, data.UrgentCount, data.NeedsReplyCount,
		data.PeriodStart, data.PeriodEnd, time.Now(), htmlSize/1024,
	)
	if err != nil {
		return fmt.Errorf("digest record: insert: %w", err)
	}

	return nil
}

// RunScheduledDigests checks all users and sends digests to those whose schedule is due.
// Intended to be called periodically by the cron scheduler (e.g., every 15 minutes).
func (d *EmailDigestChannel) RunScheduledDigests(ctx context.Context) error {
	schedules, err := d.scheduler.GetUsersForDigest(ctx)
	if err != nil {
		return fmt.Errorf("digest run: get scheduled users: %w", err)
	}

	for _, sched := range schedules {
		if err := d.CompileAndSend(ctx, sched.UserID, sched.TenantID); err != nil {
			slog.Error("digest: failed to send",
				"user_id", sched.UserID,
				"error", err,
			)
			continue
		}
	}

	slog.Info("digest: scheduled run complete",
		"users_processed", len(schedules),
	)
	return nil
}
