package auth

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
)

const MaxConcurrentSessions = 5

type Session struct {
	ID           uuid.UUID `json:"id"`
	SessionID    string    `json:"session_id"`
	DeviceName   string    `json:"device_name"`
	UserAgent    string    `json:"user_agent"`
	IPAddress    string    `json:"ip_address"`
	LastActiveAt string    `json:"last_active_at"`
	CreatedAt    string    `json:"created_at"`
}

type SessionService struct {
	pool     *pgxpool.Pool
	supabase *SupabaseClient
}

func NewSessionService(pool *pgxpool.Pool, supabase *SupabaseClient) *SessionService {
	return &SessionService{pool: pool, supabase: supabase}
}

func (s *SessionService) TrackSession(ctx database.TenantContext, userID uuid.UUID, sessionID, userAgent, ipAddress string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	// Count existing sessions
	var count int
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM auth_sessions WHERE user_id = $1`, userID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("count sessions: %w", err)
	}

	// If at limit, revoke oldest
	if count >= MaxConcurrentSessions {
		var oldestSessionID string
		err := conn.QueryRow(ctx,
			`SELECT session_id FROM auth_sessions
			 WHERE user_id = $1 ORDER BY last_active_at ASC LIMIT 1`, userID,
		).Scan(&oldestSessionID)
		if err != nil {
			return fmt.Errorf("find oldest session: %w", err)
		}
		_, _ = conn.Exec(ctx,
			`DELETE FROM auth_sessions WHERE session_id = $1`, oldestSessionID)
	}

	// Insert new session
	deviceName := parseDeviceName(userAgent)
	_, err = conn.Exec(ctx,
		`INSERT INTO auth_sessions (user_id, tenant_id, session_id, device_name, user_agent, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6::inet)
		 ON CONFLICT (session_id) DO UPDATE SET last_active_at = now()`,
		userID, ctx.TenantID, sessionID, deviceName, userAgent, ipAddress,
	)
	return err
}

func (s *SessionService) ListSessions(ctx database.TenantContext, userID uuid.UUID) ([]Session, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting tenant context: %w", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT id, session_id, device_name, user_agent, ip_address::text, last_active_at::text, created_at::text
		 FROM auth_sessions WHERE user_id = $1 ORDER BY last_active_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.SessionID, &sess.DeviceName, &sess.UserAgent, &sess.IPAddress, &sess.LastActiveAt, &sess.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (s *SessionService) RevokeAll(ctx database.TenantContext, userID uuid.UUID) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`DELETE FROM auth_sessions WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return s.supabase.SignOutUser(ctx, userID, "global")
}

func (s *SessionService) RevokeSession(ctx database.TenantContext, sessionID string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`DELETE FROM auth_sessions WHERE session_id = $1`, sessionID)
	return err
}

func (s *SessionService) UpdateLastActive(ctx database.TenantContext, sessionID string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`UPDATE auth_sessions SET last_active_at = now() WHERE session_id = $1`, sessionID)
	return err
}

func parseDeviceName(userAgent string) string {
	if len(userAgent) > 100 {
		return userAgent[:100]
	}
	return userAgent
}
