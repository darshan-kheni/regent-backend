package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// PipelineHandlers serves AI pipeline status for the live status widget.
type PipelineHandlers struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func NewPipelineHandlers(pool *pgxpool.Pool, rdb *redis.Client) *PipelineHandlers {
	return &PipelineHandlers{pool: pool, rdb: rdb}
}

type pipelineJob struct {
	EmailID   string `json:"email_id"`
	Subject   string `json:"subject"`
	Stage     string `json:"stage"`
	StartedAt string `json:"started_at"`
	Position  int    `json:"position,omitempty"`
	EstStart  string `json:"est_start,omitempty"`
}

type pipelineRecent struct {
	EmailID     string `json:"email_id"`
	Subject     string `json:"subject"`
	Stage       string `json:"stage"`
	CompletedAt string `json:"completed_at"`
	HasDraft    bool   `json:"has_draft"`
}

type pipelineStats struct {
	Total      int `json:"total"`
	Complete   int `json:"complete"`
	Processing int `json:"processing"`
	Queued     int `json:"queued"`
	Error      int `json:"error"`
}

// HandlePipelineStatus returns the current AI pipeline state for the authenticated user.
func (h *PipelineHandlers) HandlePipelineStatus(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("pipeline status: acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("pipeline status: set rls", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Active jobs (currently processing)
	activeRows, err := conn.Query(tc,
		`SELECT s.email_id, COALESCE(e.subject, 'Untitled'), s.stage, s.started_at
		 FROM email_ai_status s
		 JOIN emails e ON e.id = s.email_id
		 WHERE s.user_id = $1 AND s.stage IN ('categorizing', 'summarizing', 'drafting')
		 ORDER BY s.started_at DESC LIMIT 5`, tc.UserID)
	if err != nil {
		slog.Error("pipeline status: query active", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed")
		return
	}
	defer activeRows.Close()

	var active []pipelineJob
	for activeRows.Next() {
		var j pipelineJob
		var startedAt time.Time
		if err := activeRows.Scan(&j.EmailID, &j.Subject, &j.Stage, &startedAt); err != nil {
			continue
		}
		j.Subject = decodeMIME(j.Subject)
		j.StartedAt = startedAt.Format(time.RFC3339)
		active = append(active, j)
	}

	// Recent completions (last 10)
	recentRows, err := conn.Query(tc,
		`SELECT s.email_id, COALESCE(e.subject, 'Untitled'), s.stage, COALESCE(s.completed_at, s.started_at),
		        EXISTS(SELECT 1 FROM draft_replies d WHERE d.email_id = s.email_id LIMIT 1) as has_draft
		 FROM email_ai_status s
		 JOIN emails e ON e.id = s.email_id
		 WHERE s.user_id = $1 AND s.stage IN ('complete', 'error')
		 ORDER BY COALESCE(s.completed_at, s.started_at) DESC LIMIT 10`, tc.UserID)
	if err != nil {
		slog.Error("pipeline status: query recent", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "query failed")
		return
	}
	defer recentRows.Close()

	var recent []pipelineRecent
	for recentRows.Next() {
		var rc pipelineRecent
		var completedAt time.Time
		if err := recentRows.Scan(&rc.EmailID, &rc.Subject, &rc.Stage, &completedAt, &rc.HasDraft); err != nil {
			continue
		}
		rc.Subject = decodeMIME(rc.Subject)
		rc.CompletedAt = completedAt.Format(time.RFC3339)
		recent = append(recent, rc)
	}

	// Stats breakdown
	var stats pipelineStats
	statsRows, err := conn.Query(tc,
		`SELECT
			CASE
				WHEN stage IN ('categorizing','summarizing','drafting') THEN 'processing'
				WHEN stage = 'queued' THEN 'queued'
				WHEN stage = 'error' THEN 'error'
				WHEN stage = 'complete' THEN 'complete'
				ELSE 'other'
			END as bucket, COUNT(*)
		 FROM email_ai_status WHERE user_id = $1
		 GROUP BY bucket`, tc.UserID)
	if err == nil {
		defer statsRows.Close()
		for statsRows.Next() {
			var bucket string
			var cnt int
			if err := statsRows.Scan(&bucket, &cnt); err != nil {
				continue
			}
			switch bucket {
			case "complete":
				stats.Complete = cnt
			case "processing":
				stats.Processing = cnt
			case "queued":
				stats.Queued = cnt
			case "error":
				stats.Error = cnt
			}
			stats.Total += cnt
		}
	}

	// Average processing time per email (for ETA calculation).
	// IMPORTANT: started_at is set when the row is created (queued), not when processing begins.
	// So (completed_at - started_at) includes queue wait time and is NOT useful for processing ETA.
	// Instead, use only recent completions (last 1 hour) with a sane cap, or fall back to default.
	var avgSecsPerEmail float64 = 15 // fallback: ~15s per email (categorize + summarize + draft)
	_ = conn.QueryRow(tc,
		`SELECT COALESCE(AVG(LEAST(EXTRACT(EPOCH FROM (completed_at - started_at)), 120)), 15)
		 FROM email_ai_status
		 WHERE user_id = $1 AND stage = 'complete'
		   AND completed_at IS NOT NULL AND started_at IS NOT NULL
		   AND completed_at > NOW() - INTERVAL '1 hour'`,
		tc.UserID).Scan(&avgSecsPerEmail)
	// Clamp to reasonable bounds: 5s minimum, 60s maximum per email
	if avgSecsPerEmail < 5 {
		avgSecsPerEmail = 5
	}
	if avgSecsPerEmail > 60 {
		avgSecsPerEmail = 60
	}

	// How many are currently processing (affects when queue starts)
	activeCount := len(active)

	// Queued jobs (in email_ai_status with stage='queued')
	queuedRows, err := conn.Query(tc,
		`SELECT s.email_id, COALESCE(e.subject, 'Untitled'), s.started_at
		 FROM email_ai_status s
		 JOIN emails e ON e.id = s.email_id
		 WHERE s.user_id = $1 AND s.stage = 'queued'
		 ORDER BY s.started_at ASC LIMIT 10`, tc.UserID)
	var queued []pipelineJob
	if err == nil {
		defer queuedRows.Close()
		for queuedRows.Next() {
			var j pipelineJob
			var startedAt time.Time
			if err := queuedRows.Scan(&j.EmailID, &j.Subject, &startedAt); err != nil {
				continue
			}
			j.Subject = decodeMIME(j.Subject)
			j.Stage = "queued"
			j.StartedAt = startedAt.Format(time.RFC3339)
			queued = append(queued, j)
		}
	}

	// Unprocessed emails (no email_ai_status row yet) — also part of queue
	unprocessedRows, err := conn.Query(tc,
		`SELECT e.id, COALESCE(e.subject, 'Untitled'), e.received_at
		 FROM emails e
		 LEFT JOIN email_ai_status s ON e.id = s.email_id
		 WHERE e.user_id = $1 AND s.email_id IS NULL
		 ORDER BY e.received_at DESC LIMIT 10`, tc.UserID)
	if err == nil {
		defer unprocessedRows.Close()
		for unprocessedRows.Next() {
			var j pipelineJob
			var receivedAt time.Time
			if err := unprocessedRows.Scan(&j.EmailID, &j.Subject, &receivedAt); err != nil {
				continue
			}
			j.Subject = decodeMIME(j.Subject)
			j.Stage = "queued"
			j.StartedAt = receivedAt.Format(time.RFC3339)
			queued = append(queued, j)
		}
	}

	// Queue depth: DB queued + Redis queue + unprocessed emails
	var dbQueuedCount int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM email_ai_status WHERE user_id = $1 AND stage = 'queued'`, tc.UserID).Scan(&dbQueuedCount)

	var redisQueueDepth int64
	if h.rdb != nil {
		redisQueueDepth, _ = h.rdb.LLen(context.Background(), "ai_queue:"+tc.UserID.String()).Result()
	}
	var unprocessedCount int
	_ = conn.QueryRow(tc,
		`SELECT COUNT(*) FROM emails e
		 LEFT JOIN email_ai_status s ON e.id = s.email_id
		 WHERE e.user_id = $1 AND s.email_id IS NULL`, tc.UserID).Scan(&unprocessedCount)

	queueDepth := int64(dbQueuedCount) + redisQueueDepth + int64(unprocessedCount)

	// Realistic ETA calculation:
	// - The ai_queue cron fires every 5 minutes and picks up max 5 emails per tick.
	// - Only items in Redis are being actively processed (worker pool capacity=1, serial).
	// - Items NOT in Redis (DB queued or unprocessed) wait for the next cron tick.
	// - Each email takes ~avgSecsPerEmail to process (categorize + summarize + draft).
	//
	// We check cron_jobs.next_run for the ai_queue job to know when the next batch fires.
	const cronInterval = 5 * 60 // 5 minutes in seconds
	const batchSize = 5

	var nextCronRun *time.Time
	_ = conn.QueryRow(tc,
		`SELECT next_run FROM cron_jobs
		 WHERE user_id = $1 AND job_type = 'ai_queue' AND next_run IS NOT NULL
		 ORDER BY next_run DESC LIMIT 1`, tc.UserID).Scan(&nextCronRun)

	now := time.Now()

	// How many items are currently in Redis (actively being served by the worker)?
	inRedis := int(redisQueueDepth)

	for i := range queued {
		pos := i + 1
		queued[i].Position = pos

		var secsUntilStart float64

		if pos <= inRedis {
			// This item is already in Redis — will be processed by the worker.
			// Wait for active job to finish + items ahead in Redis queue.
			secsUntilStart = float64(activeCount)*avgSecsPerEmail + float64(i)*avgSecsPerEmail
		} else {
			// This item is NOT in Redis yet — needs a cron tick to be enqueued.
			// Figure out which cron batch this item falls into.
			waitingPos := pos - inRedis // position among items waiting for cron
			batchIndex := (waitingPos - 1) / batchSize // which cron tick picks this up (0-indexed)

			// Time until the cron tick that enqueues this item
			var secsUntilCronTick float64
			if nextCronRun != nil && nextCronRun.After(now) {
				// First tick: use actual next_run from DB
				secsUntilCronTick = nextCronRun.Sub(now).Seconds() + float64(batchIndex)*float64(cronInterval)
			} else {
				// No next_run data — estimate based on cron interval
				secsUntilCronTick = float64(batchIndex+1) * float64(cronInterval)
			}

			// Position within the batch (0-4)
			posInBatch := (waitingPos - 1) % batchSize

			// After cron enqueues, the worker still has to drain earlier items.
			// Items already in Redis get processed first, then this batch.
			redisProcessTime := float64(inRedis) * avgSecsPerEmail
			if secsUntilCronTick > redisProcessTime {
				// Cron fires after Redis is drained — just add position in batch
				secsUntilStart = secsUntilCronTick + float64(posInBatch)*avgSecsPerEmail
			} else {
				// Redis won't be drained by the time cron fires
				secsUntilStart = redisProcessTime + float64(posInBatch)*avgSecsPerEmail
			}

			// Add time for active jobs to finish
			secsUntilStart += float64(activeCount) * avgSecsPerEmail
		}

		estStart := now.Add(time.Duration(secsUntilStart * float64(time.Second)))
		queued[i].EstStart = estStart.Format(time.RFC3339)
	}

	if active == nil {
		active = []pipelineJob{}
	}
	if recent == nil {
		recent = []pipelineRecent{}
	}
	if queued == nil {
		queued = []pipelineJob{}
	}

	resp := map[string]interface{}{
		"active":             active,
		"recent":             recent,
		"queued":             queued,
		"stats":              stats,
		"queue_depth":        queueDepth,
		"avg_secs_per_email": int(avgSecsPerEmail),
		"redis_queue_depth":  inRedis,
		"cron_interval_secs": cronInterval,
		"cron_batch_size":    batchSize,
	}
	if nextCronRun != nil {
		resp["next_cron_run"] = nextCronRun.Format(time.RFC3339)
	}
	WriteJSON(w, r, http.StatusOK, resp)
}
