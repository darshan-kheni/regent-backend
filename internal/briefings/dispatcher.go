package briefings

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// BriefingDispatcher consumes events from the Redis "notification_events" stream
// and dispatches them to the appropriate notification channels.
// Runs as a single per-server goroutine (NOT per-user).
type BriefingDispatcher struct {
	rdb      *redis.Client
	pool     *pgxpool.Pool
	router   *NotificationRouter
	rules    *PriorityRulesEngine
	limiter  *RateLimiter
	tracker  *DeliveryTracker
	stream   string
	group    string
	consumer string
}

// NewBriefingDispatcher creates a new dispatcher.
func NewBriefingDispatcher(
	rdb *redis.Client,
	pool *pgxpool.Pool,
	router *NotificationRouter,
	rules *PriorityRulesEngine,
	limiter *RateLimiter,
	tracker *DeliveryTracker,
) *BriefingDispatcher {
	hostname, _ := os.Hostname()
	return &BriefingDispatcher{
		rdb:      rdb,
		pool:     pool,
		router:   router,
		rules:    rules,
		limiter:  limiter,
		tracker:  tracker,
		stream:   "notification_events",
		group:    "briefing_dispatchers",
		consumer: fmt.Sprintf("dispatcher-%s-%d", hostname, os.Getpid()),
	}
}

// Run starts the dispatcher loop. It creates the consumer group, starts
// background maintenance goroutines, and reads events in a blocking loop.
func (d *BriefingDispatcher) Run(ctx context.Context) error {
	if d.rdb == nil {
		slog.Warn("briefing dispatcher: Redis not available, skipping")
		<-ctx.Done()
		return ctx.Err()
	}

	// Create consumer group (ignore error if it already exists).
	d.rdb.XGroupCreateMkStream(ctx, d.stream, d.group, "0")

	// Background: reclaim stuck messages every 5 minutes.
	go d.runAutoClaim(ctx)

	// Background: trim stream hourly (MAXLEN ~ 10000).
	go d.runTrimmer(ctx)

	slog.Info("briefing dispatcher started",
		"stream", d.stream, "group", d.group, "consumer", d.consumer)

	for {
		select {
		case <-ctx.Done():
			slog.Info("briefing dispatcher stopping")
			return ctx.Err()
		default:
		}

		results, err := d.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    d.group,
			Consumer: d.consumer,
			Streams:  []string{d.stream, ">"},
			Count:    10,
			Block:    5 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			slog.Error("briefing dispatcher: xreadgroup error", "error", err)
			time.Sleep(1 * time.Second)
			continue
		}

		for _, stream := range results {
			for _, msg := range stream.Messages {
				d.processMessage(ctx, msg)
			}
		}
	}
}

// processMessage handles a single event from the stream.
func (d *BriefingDispatcher) processMessage(ctx context.Context, msg redis.XMessage) {
	briefing, err := deserializeBriefing(msg)
	if err != nil {
		slog.Error("briefing dispatcher: failed to deserialize", "msg_id", msg.ID, "error", err)
		d.rdb.XAck(ctx, d.stream, d.group, msg.ID)
		return
	}

	// Apply rules engine to adjust priority.
	if d.rules != nil {
		briefing.Priority = d.rules.Evaluate(ctx, briefing)
	}

	// Suppressed by rules (priority 0).
	if briefing.Priority == 0 {
		d.rdb.XAck(ctx, d.stream, d.group, msg.ID)
		return
	}

	// Load recipient info.
	recipient, err := d.loadRecipient(ctx, briefing.UserID)
	if err != nil {
		slog.Error("briefing dispatcher: load recipient failed",
			"user_id", briefing.UserID, "error", err)
		// Don't ACK — will be retried via XAUTOCLAIM.
		return
	}

	// Route to channels based on adjusted priority.
	channels := d.router.Route(ctx, briefing)

	for _, ch := range channels {
		if !d.limiter.Allow(ctx, ch.Name(), briefing.UserID) {
			slog.Warn("briefing dispatcher: rate limited",
				"channel", ch.Name(), "user_id", briefing.UserID)
			continue
		}

		if err := ch.Send(ctx, *recipient, briefing); err != nil {
			slog.Error("briefing dispatcher: channel send failed",
				"channel", ch.Name(), "user_id", briefing.UserID, "error", err)
			d.tracker.LogSend(ctx, briefing, ch.Name(), "", 0, err.Error())
			continue
		}

		d.tracker.LogSend(ctx, briefing, ch.Name(), "", 0, "")
	}

	d.rdb.XAck(ctx, d.stream, d.group, msg.ID)
}

// loadRecipient loads notification delivery addresses for a user.
func (d *BriefingDispatcher) loadRecipient(ctx context.Context, userID uuid.UUID) (*Recipient, error) {
	r := &Recipient{UserID: userID}

	if d.pool == nil {
		return r, nil
	}

	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	// Load phone/WhatsApp/Signal from prefs.
	_ = conn.QueryRow(ctx,
		`SELECT COALESCE(sms_phone, ''), COALESCE(whatsapp_phone, ''), COALESCE(signal_id, '')
		 FROM user_notification_prefs WHERE user_id = $1`, userID,
	).Scan(&r.Phone, &r.WhatsAppID, &r.SignalID)

	// Load email from users table.
	_ = conn.QueryRow(ctx,
		`SELECT COALESCE(email, '') FROM users WHERE id = $1`, userID,
	).Scan(&r.Email)

	// Load device tokens.
	rows, err := conn.Query(ctx,
		`SELECT token FROM device_tokens WHERE user_id = $1`, userID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var token string
			if rows.Scan(&token) == nil {
				r.DeviceTokens = append(r.DeviceTokens, token)
			}
		}
	}

	return r, nil
}

// deserializeBriefing converts a Redis stream message into a Briefing struct.
func deserializeBriefing(msg redis.XMessage) (Briefing, error) {
	b := Briefing{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
	}

	if v, ok := msg.Values["user_id"]; ok {
		uid, err := uuid.Parse(fmt.Sprint(v))
		if err != nil {
			return b, fmt.Errorf("parsing user_id: %w", err)
		}
		b.UserID = uid
	}

	if v, ok := msg.Values["tenant_id"]; ok {
		tid, err := uuid.Parse(fmt.Sprint(v))
		if err != nil {
			return b, fmt.Errorf("parsing tenant_id: %w", err)
		}
		b.TenantID = tid
	}

	if v, ok := msg.Values["email_id"]; ok {
		eid, err := uuid.Parse(fmt.Sprint(v))
		if err != nil {
			return b, fmt.Errorf("parsing email_id: %w", err)
		}
		b.EmailID = eid
	}

	if v, ok := msg.Values["priority"]; ok {
		p, _ := strconv.Atoi(fmt.Sprint(v))
		b.Priority = p
	}

	if v, ok := msg.Values["category"]; ok {
		b.Category = fmt.Sprint(v)
	}
	if v, ok := msg.Values["sender"]; ok {
		b.SenderName = fmt.Sprint(v)
	}
	if v, ok := msg.Values["subject"]; ok {
		b.Subject = fmt.Sprint(v)
	}
	if v, ok := msg.Values["summary"]; ok {
		b.Summary = fmt.Sprint(v)
	}

	return b, nil
}

// runAutoClaim reclaims stuck messages every 5 minutes.
func (d *BriefingDispatcher) runAutoClaim(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msgs, _, err := d.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   d.stream,
				Group:    d.group,
				Consumer: d.consumer,
				MinIdle:  60 * time.Second,
				Start:    "0-0",
				Count:    50,
			}).Result()
			if err != nil {
				slog.Debug("briefing dispatcher: xautoclaim", "error", err)
				continue
			}
			if len(msgs) > 0 {
				slog.Info("briefing dispatcher: reclaimed stuck messages", "count", len(msgs))
			}
		}
	}
}

// runTrimmer trims the stream hourly to prevent unbounded growth.
func (d *BriefingDispatcher) runTrimmer(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.rdb.XTrimMaxLenApprox(ctx, d.stream, 10000, 0)
		}
	}
}
