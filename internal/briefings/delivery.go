package briefings

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DeliveryTracker logs notification delivery events to the notification_log table.
type DeliveryTracker struct {
	pool *pgxpool.Pool
}

// NewDeliveryTracker creates a new delivery tracker.
func NewDeliveryTracker(pool *pgxpool.Pool) *DeliveryTracker {
	return &DeliveryTracker{pool: pool}
}

// LogSend inserts a notification_log entry when a briefing is sent (or fails).
func (d *DeliveryTracker) LogSend(ctx context.Context, b Briefing,
	channel, externalID string, costCents int, errMsg string) {

	if d.pool == nil {
		return
	}

	status := "sent"
	if errMsg != "" {
		status = "failed"
	}

	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		slog.Error("delivery tracker: acquire connection", "error", err)
		return
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO notification_log
		 (user_id, tenant_id, briefing_id, email_id, channel, status,
		  priority, external_id, cost_cents, error_message, sent_at, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())`,
		b.UserID, b.TenantID, b.ID, b.EmailID,
		channel, status, b.Priority, externalID, costCents, errMsg)
	if err != nil {
		slog.Error("delivery tracker: insert notification_log", "error", err)
	}
}

// UpdateStatus updates a notification_log entry when a webhook callback arrives.
func (d *DeliveryTracker) UpdateStatus(ctx context.Context,
	externalID, status string, deliveredAt, readAt *time.Time) error {

	if d.pool == nil {
		return nil
	}

	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`UPDATE notification_log SET status=$1, delivered_at=$2, read_at=$3
		 WHERE external_id=$4`,
		status, deliveredAt, readAt, externalID)
	return err
}
