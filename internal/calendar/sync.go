package calendar

import (
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

// TokenStore interface decouples from the auth package.
// Must match OAuthTokenStore.StoreTokens signature (scopes is []string).
type TokenStore interface {
	StoreTokens(ctx database.TenantContext, userID uuid.UUID, provider string,
		accessToken, refreshToken string, scopes []string, expiresAt time.Time,
		providerUserID, providerEmail string) error
	MarkRevoked(userID uuid.UUID, provider string, accountID uuid.UUID) error
}

// TokenGetter retrieves decrypted OAuth tokens for a given user and provider.
// Implemented by auth.OAuthTokenStore.
type TokenGetter interface {
	GetTokens(ctx database.TenantContext, userID uuid.UUID, provider string) (*OAuthTokenResult, error)
}

// OAuthTokenResult holds decrypted OAuth token data retrieved from the token store.
type OAuthTokenResult struct {
	AccessToken   string
	RefreshToken  string
	ExpiresAt     time.Time
	ProviderEmail string
}

// GoogleOAuthCreds holds the Google OAuth2 client credentials needed for token refresh.
type GoogleOAuthCreds struct {
	ClientID     string
	ClientSecret string
}

// PersistingTokenSource wraps oauth2.TokenSource to auto-persist refreshed tokens.
type PersistingTokenSource struct {
	base      oauth2.TokenSource
	tc        database.TenantContext
	accountID uuid.UUID
	provider  string // "google_calendar" | "microsoft_calendar"
	email     string
	store     TokenStore
	mu        sync.Mutex
	current   *oauth2.Token
}

// NewPersistingTokenSource creates a token source that saves refreshed tokens to the DB.
func NewPersistingTokenSource(base oauth2.TokenSource, tc database.TenantContext,
	accountID uuid.UUID, provider, email string, store TokenStore) *PersistingTokenSource {
	return &PersistingTokenSource{
		base:      base,
		tc:        tc,
		accountID: accountID,
		provider:  provider,
		email:     email,
		store:     store,
	}
}

// Token returns a valid token, persisting any refreshed tokens to the database.
func (p *PersistingTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tok, err := p.base.Token()
	if err != nil {
		if isTokenRevoked(err) {
			_ = p.store.MarkRevoked(p.tc.UserID, p.provider, p.accountID)
			return nil, fmt.Errorf("oauth consent revoked: %w", err)
		}
		return nil, err
	}
	if p.current == nil || tok.AccessToken != p.current.AccessToken {
		if saveErr := p.store.StoreTokens(
			p.tc, p.tc.UserID, p.provider,
			tok.AccessToken, tok.RefreshToken,
			nil, tok.Expiry,
			p.tc.UserID.String(), p.email,
		); saveErr != nil {
			slog.Error("failed to persist token", "user_id", p.tc.UserID, "err", saveErr)
		}
		p.current = tok
	}
	return tok, nil
}

func isTokenRevoked(err error) bool {
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) {
		body := string(retrieveErr.Body)
		return strings.Contains(body, "invalid_grant") ||
			strings.Contains(body, "interaction_required")
	}
	return false
}

// RetryPolicy implements exponential backoff for API calls.
type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// Do executes fn with exponential backoff retries on transient errors.
func (rp *RetryPolicy) Do(fn func() error) error {
	for attempt := 0; attempt <= rp.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !isRetryable(err) || attempt == rp.MaxRetries {
			return err
		}
		delay := rp.BaseDelay * time.Duration(1<<uint(attempt))
		if ra := extractRetryAfter(err); ra > 0 {
			delay = ra
		}
		if delay > rp.MaxDelay {
			delay = rp.MaxDelay
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("max retries exceeded")
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") ||
		strings.Contains(s, "500") ||
		strings.Contains(s, "502") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "timeout")
}

func extractRetryAfter(err error) time.Duration {
	// Check for HTTP response with Retry-After header.
	type httpError interface {
		Header() http.Header
	}
	var he httpError
	if errors.As(err, &he) {
		if ra := he.Header().Get("Retry-After"); ra != "" {
			if seconds, parseErr := strconv.Atoi(ra); parseErr == nil {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return 0
}

// SyncEngine coordinates calendar sync with advisory locks to prevent concurrent syncs.
type SyncEngine struct {
	pool        *pgxpool.Pool
	retryPolicy *RetryPolicy
	cfg         *config.CalendarConfig
	tokenStore  TokenStore
	tokenGetter TokenGetter
	googleCreds *GoogleOAuthCreds
}

// NewSyncEngine creates a SyncEngine with retry policy derived from config.
func NewSyncEngine(pool *pgxpool.Pool, cfg *config.CalendarConfig, tokenStore TokenStore, tokenGetter TokenGetter, googleCreds *GoogleOAuthCreds) *SyncEngine {
	return &SyncEngine{
		pool: pool,
		cfg:  cfg,
		retryPolicy: &RetryPolicy{
			MaxRetries: cfg.RetryMaxAttempts,
			BaseDelay:  time.Duration(cfg.RetryBaseDelaySeconds) * time.Second,
			MaxDelay:   30 * time.Second,
		},
		tokenStore:  tokenStore,
		tokenGetter: tokenGetter,
		googleCreds: googleCreds,
	}
}

// SyncWithLock acquires a PostgreSQL advisory lock and runs the provider-specific sync.
// If another goroutine already holds the lock for this user+account, the call is skipped.
func (s *SyncEngine) SyncWithLock(ctx database.TenantContext, accountID uuid.UUID, provider string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection for sync: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sync tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	lockKey := int64(hashUUIDs(ctx.UserID, accountID))
	var acquired bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", lockKey).Scan(&acquired); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	if !acquired {
		slog.Debug("sync already running, skipping", "user_id", ctx.UserID, "provider", provider)
		return nil
	}

	// Set RLS context within the transaction.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", ctx.TenantID.String()); err != nil {
		return fmt.Errorf("set RLS context: %w", err)
	}

	var syncErr error
	switch provider {
	case "google":
		syncErr = s.syncGoogle(ctx, accountID)
	case "microsoft":
		syncErr = s.syncMicrosoft(ctx, accountID)
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}

	if syncErr != nil {
		return syncErr
	}
	return tx.Commit(ctx)
}

// hashUUIDs creates a deterministic int64 from two UUIDs for advisory lock keys.
func hashUUIDs(a, b uuid.UUID) uint64 {
	h := fnv.New64a()
	h.Write(a[:])
	h.Write(b[:])
	return h.Sum64()
}

// --- DB helper methods (used by provider-specific sync in google.go / microsoft.go) ---

func (s *SyncEngine) getSyncState(ctx database.TenantContext, accountID uuid.UUID, provider string) (*SyncState, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, err
	}

	var state SyncState
	err = conn.QueryRow(ctx,
		`SELECT id, user_id, tenant_id, account_id, provider, sync_token, status, last_sync
		 FROM calendar_sync_state WHERE account_id = $1 AND provider = $2`,
		accountID, provider,
	).Scan(&state.ID, &state.UserID, &state.TenantID, &state.AccountID,
		&state.Provider, &state.SyncToken, &state.Status, &state.LastSync)
	if err != nil {
		// No sync state yet — return empty with defaults.
		return &SyncState{AccountID: accountID, Provider: provider, Status: "active"}, nil
	}
	return &state, nil
}

func (s *SyncEngine) saveSyncState(ctx database.TenantContext, accountID uuid.UUID, provider, syncToken string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO calendar_sync_state (user_id, tenant_id, account_id, provider, sync_token, last_sync, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now(), now())
		 ON CONFLICT (account_id, provider) DO UPDATE
		 SET sync_token = $5, last_sync = now(), updated_at = now(), status = 'active'`,
		ctx.UserID, ctx.TenantID, accountID, provider, syncToken,
	)
	return err
}

func (s *SyncEngine) clearSyncState(ctx database.TenantContext, accountID uuid.UUID, provider string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`UPDATE calendar_sync_state SET sync_token = '', status = 'active', updated_at = now()
		 WHERE account_id = $1 AND provider = $2`,
		accountID, provider,
	)
	return err
}

func (s *SyncEngine) upsertEvent(ctx database.TenantContext, event CalendarEvent) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO calendar_events (
			user_id, tenant_id, account_id, provider, provider_event_id, calendar_id,
			title, description, start_time, end_time, time_zone, location,
			is_all_day, status, attendees, recurrence_rule, organizer_email,
			is_online, meeting_url, last_synced
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,now())
		ON CONFLICT (account_id, provider_event_id) DO UPDATE SET
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			start_time = EXCLUDED.start_time,
			end_time = EXCLUDED.end_time,
			time_zone = EXCLUDED.time_zone,
			location = EXCLUDED.location,
			is_all_day = EXCLUDED.is_all_day,
			status = EXCLUDED.status,
			attendees = EXCLUDED.attendees,
			recurrence_rule = EXCLUDED.recurrence_rule,
			organizer_email = EXCLUDED.organizer_email,
			is_online = EXCLUDED.is_online,
			meeting_url = EXCLUDED.meeting_url,
			last_synced = now(),
			updated_at = now()`,
		event.UserID, event.TenantID, event.AccountID, event.Provider,
		event.ProviderEventID, event.CalendarID, event.Title, event.Description,
		event.StartTime, event.EndTime, event.TimeZone, event.Location,
		event.IsAllDay, event.Status, event.Attendees, event.RecurrenceRule,
		event.OrganizerEmail, event.IsOnline, event.MeetingURL,
	)
	return err
}

func (s *SyncEngine) deleteEvent(ctx database.TenantContext, accountID uuid.UUID, providerEventID string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return err
	}

	_, err = conn.Exec(ctx,
		`DELETE FROM calendar_events WHERE account_id = $1 AND provider_event_id = $2`,
		accountID, providerEventID,
	)
	return err
}

// syncMicrosoft is implemented in microsoft.go.
