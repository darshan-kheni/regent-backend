package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/queue"
	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/database"
	emailpkg "github.com/darshan-kheni/regent/internal/email"
	"github.com/darshan-kheni/regent/internal/email/connection"
	imappkg "github.com/darshan-kheni/regent/internal/email/imap"
	"github.com/darshan-kheni/regent/internal/email/sync"
	"github.com/darshan-kheni/regent/internal/models"
)

// CronScheduler manages periodic jobs for a single user.
// Currently supports email polling (2min). Phase 4+ will add:
//   - AI queue processing (5min)
//   - Token aggregation (1hr)
//   - Nightly batch (24hr)
//   - Weekly synthesis (7d)
type CronScheduler struct {
	userID     uuid.UUID
	tenantID   uuid.UUID
	pool       *pgxpool.Pool
	credStore  *emailpkg.CredentialStore
	rdb        *redis.Client
	aiProvider ai.AIProvider
}

// jobDefinition describes a scheduled job's type and interval.
type jobDefinition struct {
	jobType  string
	interval time.Duration
	fn       func(ctx context.Context) error
}

// NewCronScheduler creates a scheduler for the given user.
func NewCronScheduler(userID, tenantID uuid.UUID, pool *pgxpool.Pool, encryptor *crypto.RotatingEncryptor, aiProvider ai.AIProvider, rdb *redis.Client) *CronScheduler {
	var credStore *emailpkg.CredentialStore
	if encryptor != nil {
		credStore = emailpkg.NewCredentialStore(encryptor, pool)
	}
	return &CronScheduler{
		userID:     userID,
		tenantID:   tenantID,
		pool:       pool,
		credStore:  credStore,
		rdb:        rdb,
		aiProvider: aiProvider,
	}
}

// Run starts all cron jobs. It first recovers any missed jobs, then
// enters the main tick loop. Blocks until ctx is cancelled.
func (c *CronScheduler) Run(ctx context.Context) error {
	c.recoverMissedJobs(ctx)

	// Run email poll immediately on startup so new accounts sync right away.
	c.runJob(ctx, "email_poll", c.emailPoll)

	// Run AI queue immediately to process any unprocessed emails.
	c.runJob(ctx, "ai_queue", c.aiQueueProcess)

	jobs := []jobDefinition{
		{jobType: "email_poll", interval: 2 * time.Minute, fn: c.emailPoll},
		{jobType: "ai_queue", interval: 5 * time.Minute, fn: c.aiQueueProcess},
		{jobType: "token_aggregation", interval: 1 * time.Hour, fn: c.tokenAggregation},
		{jobType: "nightly_batch", interval: 24 * time.Hour, fn: c.nightlyBatch},
		{jobType: "email_digest", interval: 6 * time.Hour, fn: c.emailDigest},
		{jobType: "grace_period_check", interval: 24 * time.Hour, fn: c.gracePeriodCheck},
		{jobType: "trial_expiry_check", interval: 24 * time.Hour, fn: c.trialExpiryCheck},
		{jobType: "nightly_behavior", interval: 24 * time.Hour, fn: c.nightlyBehavior},
		{jobType: "weekly_wellness_report", interval: 168 * time.Hour, fn: c.weeklyWellnessReport},
		{jobType: "calendar_sync", interval: 5 * time.Minute, fn: c.calendarSync},
		{jobType: "meeting_lifecycle", interval: 1 * time.Minute, fn: c.meetingLifecycle},
		{jobType: "task_reminders", interval: 15 * time.Minute, fn: c.taskReminders},
		{jobType: "task_digest", interval: 24 * time.Hour, fn: c.taskDigest},
		{jobType: "task_delegation_followup", interval: 6 * time.Hour, fn: c.taskDelegationFollowup},
		{jobType: "task_recurrence", interval: 1 * time.Hour, fn: c.taskRecurrence},
	}

	// Create a ticker for each job and run them in their own goroutines.
	type tickerJob struct {
		ticker *time.Ticker
		def    jobDefinition
	}

	tickers := make([]tickerJob, 0, len(jobs))
	for _, j := range jobs {
		tickers = append(tickers, tickerJob{
			ticker: time.NewTicker(j.interval),
			def:    j,
		})
	}

	defer func() {
		for _, tj := range tickers {
			tj.ticker.Stop()
		}
	}()

	// Multiplex all tickers into a single select loop.
	// For a small number of jobs this is cleaner than spawning N goroutines.
	for {
		// Build cases dynamically: check each ticker.
		// Since Go select can't be dynamic, we poll with a short interval.
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Check each ticker (non-blocking).
		fired := false
		for _, tj := range tickers {
			select {
			case <-tj.ticker.C:
				c.runJob(ctx, tj.def.jobType, tj.def.fn)
				fired = true
			default:
			}
		}

		if !fired {
			// Sleep briefly to avoid busy-waiting.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

// recoverMissedJobs checks for overdue jobs and runs them immediately.
// Uses direct pool access (bypasses RLS) since this is an admin operation.
func (c *CronScheduler) recoverMissedJobs(ctx context.Context) {
	if c.pool == nil {
		return
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		slog.Error("cron: failed to acquire connection for missed job recovery",
			"user_id", c.userID,
			"error", err,
		)
		return
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT job_type, last_run, next_run FROM cron_jobs
		 WHERE user_id = $1 AND status IN ('pending', 'completed')
		   AND next_run IS NOT NULL AND next_run < now()`,
		c.userID,
	)
	if err != nil {
		slog.Error("cron: failed to query missed jobs",
			"user_id", c.userID,
			"error", fmt.Errorf("querying missed jobs: %w", err),
		)
		return
	}
	defer rows.Close()

	var overdue []string
	for rows.Next() {
		var jobType string
		var lastRun, nextRun *time.Time
		if err := rows.Scan(&jobType, &lastRun, &nextRun); err != nil {
			slog.Error("cron: failed to scan missed job row",
				"user_id", c.userID,
				"error", err,
			)
			continue
		}
		overdue = append(overdue, jobType)
	}

	jobFuncs := map[string]func(context.Context) error{
		"email_poll":        c.emailPoll,
		"ai_queue":          c.aiQueueProcess,
		"token_aggregation": c.tokenAggregation,
		"nightly_batch":        c.nightlyBatch,
		"email_digest":         c.emailDigest,
		"grace_period_check":   c.gracePeriodCheck,
		"trial_expiry_check":   c.trialExpiryCheck,
		"nightly_behavior":       c.nightlyBehavior,
		"weekly_wellness_report": c.weeklyWellnessReport,
		"calendar_sync":          c.calendarSync,
		"meeting_lifecycle":      c.meetingLifecycle,
		"task_reminders":            c.taskReminders,
		"task_digest":               c.taskDigest,
		"task_delegation_followup":  c.taskDelegationFollowup,
		"task_recurrence":           c.taskRecurrence,
	}

	for _, jobType := range overdue {
		slog.Info("cron: recovering missed job",
			"user_id", c.userID,
			"job_type", jobType,
		)
		if fn, ok := jobFuncs[jobType]; ok {
			c.runJob(ctx, jobType, fn)
		}
	}
}

// runJob executes a job function and updates the cron_jobs table with timing and status.
// Bypasses RLS — admin-level write.
func (c *CronScheduler) runJob(ctx context.Context, jobType string, fn func(context.Context) error) {
	start := time.Now()

	// Mark as running.
	c.updateJobStatus(ctx, jobType, "running", "")

	err := fn(ctx)

	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		slog.Error("cron: job failed",
			"user_id", c.userID,
			"job_type", jobType,
			"duration_ms", durationMs,
			"error", fmt.Errorf("running job %s: %w", jobType, err),
		)
		c.updateJobResult(ctx, jobType, "failed", durationMs, err.Error())
		return
	}

	slog.Debug("cron: job completed",
		"user_id", c.userID,
		"job_type", jobType,
		"duration_ms", durationMs,
	)
	c.updateJobResult(ctx, jobType, "completed", durationMs, "")
}

// updateJobStatus sets the status of a cron job (e.g., to "running").
func (c *CronScheduler) updateJobStatus(ctx context.Context, jobType, status, errMsg string) {
	if c.pool == nil {
		return
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO cron_jobs (tenant_id, user_id, job_type, status, error_message)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, job_type) DO UPDATE SET
			status = EXCLUDED.status,
			error_message = EXCLUDED.error_message`,
		c.tenantID, c.userID, jobType, status, errMsg,
	)
	if err != nil {
		slog.Error("cron: failed to update job status",
			"user_id", c.userID,
			"job_type", jobType,
			"error", err,
		)
	}
}

// updateJobResult records the outcome of a completed job run.
// The interval parameter is used to schedule the next run correctly per job type.
func (c *CronScheduler) updateJobResult(ctx context.Context, jobType, status string, durationMs int64, errMsg string) {
	// Find the interval for this job type to schedule next_run correctly.
	interval := c.getJobInterval(jobType)
	if c.pool == nil {
		return
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO cron_jobs (tenant_id, user_id, job_type, status, last_run, duration_ms, error_message)
		 VALUES ($1, $2, $3, $4, now(), $5, $6)
		 ON CONFLICT (user_id, job_type) DO UPDATE SET
			status = EXCLUDED.status,
			last_run = now(),
			next_run = now() + $7::interval,
			duration_ms = EXCLUDED.duration_ms,
			error_message = EXCLUDED.error_message`,
		c.tenantID, c.userID, jobType, status, durationMs, errMsg, interval.String(),
	)
	if err != nil {
		slog.Error("cron: failed to update job result",
			"user_id", c.userID,
			"job_type", jobType,
			"error", err,
		)
	}
}

// getJobInterval returns the configured interval for a job type.
// Falls back to 2 minutes if the job type is unknown.
func (c *CronScheduler) getJobInterval(jobType string) time.Duration {
	intervals := map[string]time.Duration{
		"email_poll":           2 * time.Minute,
		"ai_queue":             5 * time.Minute,
		"token_aggregation":    1 * time.Hour,
		"nightly_batch":        24 * time.Hour,
		"email_digest":         6 * time.Hour,
		"grace_period_check":   24 * time.Hour,
		"trial_expiry_check":   24 * time.Hour,
		"nightly_behavior":       24 * time.Hour,
		"weekly_wellness_report": 168 * time.Hour,
		"calendar_sync":          5 * time.Minute,
		"meeting_lifecycle":      1 * time.Minute,
		"task_reminders":            15 * time.Minute,
		"task_digest":               24 * time.Hour,
		"task_delegation_followup":  6 * time.Hour,
		"task_recurrence":           1 * time.Hour,
	}
	if d, ok := intervals[jobType]; ok {
		return d
	}
	return 2 * time.Minute
}

// emailPoll connects to IMAP for each user account and syncs new emails.
func (c *CronScheduler) emailPoll(ctx context.Context) error {
	if c.credStore == nil {
		slog.Debug("cron: email poll skipped (no encryption configured)", "user_id", c.userID)
		return nil
	}

	tc := database.WithTenant(ctx, c.tenantID, c.userID)

	// Fetch user accounts (bypass RLS, direct query by user_id).
	accounts, err := c.getUserAccounts(ctx)
	if err != nil {
		return fmt.Errorf("fetching user accounts: %w", err)
	}

	if len(accounts) == 0 {
		slog.Debug("cron: no accounts to sync", "user_id", c.userID)
		return nil
	}

	slog.Info("cron: email poll found accounts", "user_id", c.userID, "count", len(accounts))

	// Create sync engine dependencies.
	threadSvc := emailpkg.NewThreadService(c.pool)
	dedupSvc := emailpkg.NewDedupService(c.pool)
	engine := sync.NewSyncEngine(c.pool, threadSvc, dedupSvc, nil) // nil storage = skip attachments

	for _, account := range accounts {
		if err := c.syncAccount(ctx, tc, engine, &account); err != nil {
			slog.Error("cron: email poll failed for account",
				"user_id", c.userID,
				"account_id", account.ID,
				"email", account.EmailAddress,
				"error", err,
			)
			c.updateAccountSyncStatus(ctx, account.ID, "error", err.Error())
			continue
		}
	}

	return nil
}

// syncAccount performs IMAP sync for a single account.
func (c *CronScheduler) syncAccount(ctx context.Context, tc database.TenantContext, engine *sync.SyncEngine, account *models.UserAccount) error {
	provider := account.Provider
	if provider != "gmail" && provider != "imap" && provider != "outlook" {
		slog.Debug("cron: skipping unsupported provider", "provider", provider)
		return nil
	}

	// Get IMAP password (credential store handles its own RLS).
	password, err := c.getCredentialDirect(ctx, account.ID, "imap_password")
	if err != nil {
		return fmt.Errorf("decrypting IMAP password: %w", err)
	}

	host := account.IMAPHost
	port := account.IMAPPort
	if host == "" {
		host = "imap.gmail.com"
	}
	if port == 0 {
		port = 993
	}

	slog.Info("cron: connecting IMAP",
		"user_id", c.userID,
		"account_id", account.ID,
		"host", host,
		"port", port,
	)

	// Dial IMAP.
	client, err := connection.Dial(ctx, host, port, nil)
	if err != nil {
		return fmt.Errorf("dialing IMAP %s:%d: %w", host, port, err)
	}
	defer client.Close()

	// Authenticate.
	if err := imappkg.AuthenticatePlain(client, account.EmailAddress, password); err != nil {
		return fmt.Errorf("IMAP auth for %s: %w", account.EmailAddress, err)
	}

	slog.Info("cron: IMAP authenticated, starting sync",
		"user_id", c.userID,
		"account_id", account.ID,
	)

	// Run sync.
	if err := engine.Sync(tc, account, client, nil); err != nil {
		return fmt.Errorf("sync for %s: %w", account.EmailAddress, err)
	}

	// Update account status to active.
	c.updateAccountSyncStatus(ctx, account.ID, "active", "")

	slog.Info("cron: email sync completed",
		"user_id", c.userID,
		"account_id", account.ID,
		"email", account.EmailAddress,
	)

	return nil
}

// getUserAccounts fetches all email accounts for this user.
// Uses direct query without RLS (background admin operation).
func (c *CronScheduler) getUserAccounts(ctx context.Context) ([]models.UserAccount, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT id, user_id, tenant_id, provider, email_address,
		        COALESCE(display_name, ''), COALESCE(imap_host, ''), COALESCE(imap_port, 993),
		        COALESCE(smtp_host, ''), COALESCE(smtp_port, 587), sync_status,
		        last_sync_at, COALESCE(error_message, ''), created_at
		 FROM user_accounts WHERE user_id = $1`, c.userID)
	if err != nil {
		return nil, fmt.Errorf("querying accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.UserAccount
	for rows.Next() {
		var a models.UserAccount
		if err := rows.Scan(&a.ID, &a.UserID, &a.TenantID, &a.Provider, &a.EmailAddress,
			&a.DisplayName, &a.IMAPHost, &a.IMAPPort, &a.SMTPHost, &a.SMTPPort,
			&a.SyncStatus, &a.LastSyncAt, &a.ErrorMessage, &a.CreatedAt); err != nil {
			slog.Error("cron: scan account", "error", err)
			continue
		}
		accounts = append(accounts, a)
	}

	return accounts, nil
}

// getCredentialDirect retrieves and decrypts a credential bypassing RLS.
func (c *CronScheduler) getCredentialDirect(ctx context.Context, accountID uuid.UUID, credType string) (string, error) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	var ciphertext, nonce []byte
	err = conn.QueryRow(ctx,
		`SELECT encrypted_value, encryption_nonce FROM email_credentials
		 WHERE account_id = $1 AND credential_type = $2`,
		accountID, credType).Scan(&ciphertext, &nonce)
	if err != nil {
		return "", fmt.Errorf("fetching credential: %w", err)
	}

	purpose := emailpkg.PurposeIMAPCredentials
	if credType == "smtp_password" {
		purpose = emailpkg.PurposeSMTPCredentials
	}

	plainBytes, err := c.credStore.Encryptor().DecryptForPurpose(c.tenantID, purpose, ciphertext, nonce)
	if err != nil {
		return "", fmt.Errorf("decrypting credential: %w", err)
	}
	return string(plainBytes), nil
}

// updateAccountSyncStatus updates the sync_status of an account (bypasses RLS).
func (c *CronScheduler) updateAccountSyncStatus(ctx context.Context, accountID uuid.UUID, status, errMsg string) {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	if status == "active" {
		_, _ = conn.Exec(ctx,
			`UPDATE user_accounts SET sync_status = $1, last_sync_at = now(), error_message = NULL WHERE id = $2`,
			status, accountID)
	} else {
		_, _ = conn.Exec(ctx,
			`UPDATE user_accounts SET sync_status = $1, error_message = $2 WHERE id = $3`,
			status, errMsg, accountID)
	}
}

// aiQueueProcess finds unprocessed emails for this user and enqueues them into
// the Redis-backed AI queue for the global WorkerPool to pick up.
func (c *CronScheduler) aiQueueProcess(ctx context.Context) error {
	if c.rdb == nil || c.aiProvider == nil {
		slog.Debug("cron: ai queue process skipped (no AI provider or Redis)",
			"user_id", c.userID,
		)
		return nil
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	// Find emails that need AI processing:
	// 1. Emails with no email_ai_status row (never processed)
	// 2. Emails with email_ai_status.stage = 'queued' (re-queued for reprocessing)
	// 3. Stale jobs stuck in intermediate stages for > 10 minutes (crashed mid-pipeline)
	// 4. Errored jobs with retry_count < 3 (auto-retry after 5 minutes)
	rows, err := conn.Query(ctx,
		`(SELECT e.id FROM emails e
		  LEFT JOIN email_ai_status s ON e.id = s.email_id
		  WHERE e.user_id = $1 AND s.email_id IS NULL
		  ORDER BY e.received_at DESC
		  LIMIT 5)
		 UNION ALL
		 (SELECT s.email_id FROM email_ai_status s
		  WHERE s.user_id = $1 AND s.stage = 'queued'
		  ORDER BY s.started_at ASC
		  LIMIT 5)
		 UNION ALL
		 (SELECT s.email_id FROM email_ai_status s
		  WHERE s.user_id = $1
		    AND s.stage IN ('categorizing', 'summarizing', 'drafting')
		    AND s.started_at < now() - interval '10 minutes'
		  ORDER BY s.started_at ASC
		  LIMIT 5)
		 UNION ALL
		 (SELECT s.email_id FROM email_ai_status s
		  WHERE s.user_id = $1
		    AND s.stage = 'error'
		    AND s.retry_count < 3
		    AND s.completed_at < now() - interval '5 minutes'
		  ORDER BY s.completed_at ASC
		  LIMIT 3)
		 LIMIT 5`, c.userID)
	if err != nil {
		return fmt.Errorf("querying emails for AI processing: %w", err)
	}
	defer rows.Close()

	var emailIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			slog.Error("cron: scan email id", "user_id", c.userID, "error", err)
			continue
		}
		emailIDs = append(emailIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating email rows: %w", err)
	}

	if len(emailIDs) == 0 {
		slog.Debug("cron: no emails to process", "user_id", c.userID)
		return nil
	}

	// Look up the tenant's plan for queue priority.
	var plan string
	err = conn.QueryRow(ctx,
		`SELECT COALESCE(t.plan, 'free') FROM tenants t
		 JOIN users u ON u.tenant_id = t.id
		 WHERE u.id = $1`, c.userID).Scan(&plan)
	if err != nil {
		slog.Warn("cron: failed to get tenant plan, defaulting to free",
			"user_id", c.userID,
			"error", err,
		)
		plan = "free"
	}

	// Enqueue each unprocessed email.
	enqueuer := queue.NewQueueEnqueuer(queue.NewQueue(c.rdb))
	enqueued := 0
	for _, emailID := range emailIDs {
		if err := enqueuer.EnqueueEmail(ctx, emailID, c.userID, c.tenantID, plan); err != nil {
			slog.Error("cron: failed to enqueue email for AI processing",
				"user_id", c.userID,
				"email_id", emailID,
				"error", err,
			)
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		slog.Info("cron: enqueued emails for AI processing",
			"user_id", c.userID,
			"count", enqueued,
			"plan", plan,
		)
	}

	return nil
}

// tokenAggregation aggregates token usage into the daily summary table.
func (c *CronScheduler) tokenAggregation(ctx context.Context) error {
	slog.Debug("cron: token aggregation",
		"user_id", c.userID,
	)
	return nil
}

// nightlyBatch runs nightly tasks: preference signals, RAG embeddings, behavior recompute.
func (c *CronScheduler) nightlyBatch(ctx context.Context) error {
	slog.Debug("cron: nightly batch",
		"user_id", c.userID,
	)
	return nil
}

// emailDigest generates and dispatches periodic email digest briefings.
func (c *CronScheduler) emailDigest(ctx context.Context) error {
	slog.Debug("cron: email digest (placeholder)",
		"user_id", c.userID,
	)
	return nil
}

// gracePeriodCheck checks for expired subscription grace periods and downgrades to Free.
// This is a system-level job (not per-user), but runs within each user's scheduler.
// Only the first scheduler to run it each day will find expired tenants.
func (c *CronScheduler) gracePeriodCheck(ctx context.Context) error {
	slog.Debug("cron: grace period check",
		"user_id", c.userID,
	)
	// Will be wired to billing.CheckGraceExpiry(ctx, pool, rdb)
	return nil
}

// trialExpiryCheck checks for expired promo trial periods and downgrades to Free.
func (c *CronScheduler) trialExpiryCheck(ctx context.Context) error {
	slog.Debug("cron: trial expiry check",
		"user_id", c.userID,
	)
	// Will be wired to billing.CheckTrialExpiry(ctx, pool, rdb)
	return nil
}

// nightlyBehavior runs nightly behavior intelligence computation for this user.
// Constructs TenantContext and delegates to BehaviorService.RunNightly.
func (c *CronScheduler) nightlyBehavior(ctx context.Context) error {
	slog.Debug("cron: nightly behavior computation",
		"user_id", c.userID,
	)
	// Will be wired to behavior.BehaviorService.RunNightly in T6
	return nil
}

// weeklyWellnessReport generates the weekly wellness report for this user.
// Only runs for Privy Council and Estate plans.
func (c *CronScheduler) weeklyWellnessReport(ctx context.Context) error {
	slog.Debug("cron: weekly wellness report",
		"user_id", c.userID,
	)
	// Will be wired to behavior.BehaviorService.GenerateWeeklyReport in T6
	return nil
}

// calendarSync triggers calendar sync for all connected calendar providers.
func (c *CronScheduler) calendarSync(ctx context.Context) error {
	slog.Debug("cron: calendar sync",
		"user_id", c.userID,
	)
	// Will be wired to calendar.SyncEngine in Phase 9 T1.6
	return nil
}

// meetingLifecycle handles meeting prep briefs and post-meeting prompts.
func (c *CronScheduler) meetingLifecycle(ctx context.Context) error {
	slog.Debug("cron: meeting lifecycle",
		"user_id", c.userID,
	)
	// Will be wired to calendar.MeetingLifecycle in Phase 9 T4
	return nil
}

func (c *CronScheduler) taskReminders(ctx context.Context) error {
	slog.Debug("cron: task reminders", "user_id", c.userID)
	return nil
}

func (c *CronScheduler) taskDigest(ctx context.Context) error {
	slog.Debug("cron: task digest", "user_id", c.userID)
	return nil
}

func (c *CronScheduler) taskDelegationFollowup(ctx context.Context) error {
	slog.Debug("cron: task delegation followup", "user_id", c.userID)
	return nil
}

func (c *CronScheduler) taskRecurrence(ctx context.Context) error {
	slog.Debug("cron: task recurrence", "user_id", c.userID)
	return nil
}
