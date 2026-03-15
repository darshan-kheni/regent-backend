package email

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/email/mime"
)

// ThreadService assigns emails to conversation threads.
type ThreadService struct {
	pool *pgxpool.Pool
}

// NewThreadService creates a new ThreadService.
func NewThreadService(pool *pgxpool.Pool) *ThreadService {
	return &ThreadService{pool: pool}
}

// subjectPrefixRe matches Re:/Fwd:/RE:/FW: prefixes (case-insensitive).
var subjectPrefixRe = regexp.MustCompile(`(?i)^(re|fwd|fw)\s*:\s*`)

// AssignThread determines the thread_id for an email using three strategies:
//  1. In-Reply-To header (strongest — direct parent reference)
//  2. References header (any Message-ID in the chain matches an existing thread)
//  3. Subject-based fallback (strip Re:/Fwd: prefixes, match within 7-day window)
//
// If no existing thread is found, a new thread_id is generated.
func (ts *ThreadService) AssignThread(ctx database.TenantContext, parsed *mime.ParsedEmail, accountID uuid.UUID) (uuid.UUID, error) {
	conn, err := ts.pool.Acquire(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return uuid.Nil, fmt.Errorf("setting tenant context: %w", err)
	}

	// Strategy 1: In-Reply-To — strongest signal, direct parent reference.
	if parsed.InReplyTo != "" {
		var threadID uuid.UUID
		err := conn.QueryRow(ctx,
			`SELECT thread_id FROM emails
			 WHERE account_id = $1 AND message_id = $2 AND thread_id IS NOT NULL
			 LIMIT 1`,
			accountID, parsed.InReplyTo,
		).Scan(&threadID)
		if err == nil {
			return threadID, nil
		}
	}

	// Strategy 2: References chain — walk the reference list for any known thread.
	if len(parsed.References) > 0 {
		// Check references in reverse order (most recent first) for faster matching.
		for i := len(parsed.References) - 1; i >= 0; i-- {
			var threadID uuid.UUID
			err := conn.QueryRow(ctx,
				`SELECT thread_id FROM emails
				 WHERE account_id = $1 AND message_id = $2 AND thread_id IS NOT NULL
				 LIMIT 1`,
				accountID, parsed.References[i],
			).Scan(&threadID)
			if err == nil {
				return threadID, nil
			}
		}
	}

	// Strategy 3: Subject-based fallback — strip prefixes, match within 7-day window.
	normalizedSubject := NormalizeSubject(parsed.Subject)
	if normalizedSubject != "" {
		var threadID uuid.UUID
		cutoff := parsed.ReceivedAt.AddDate(0, 0, -7)
		err := conn.QueryRow(ctx,
			`SELECT thread_id FROM emails
			 WHERE account_id = $1 AND thread_id IS NOT NULL
			 AND received_at > $2
			 AND regexp_replace(subject, '(?i)^(re|fwd|fw)\s*:\s*', '', 'g') = $3
			 ORDER BY received_at DESC
			 LIMIT 1`,
			accountID, cutoff, normalizedSubject,
		).Scan(&threadID)
		if err == nil {
			return threadID, nil
		}
	}

	// No existing thread found — create a new one.
	return uuid.New(), nil
}

// NormalizeSubject strips Re:/Fwd:/RE:/FW: prefixes and trims whitespace.
// Handles nested prefixes like "Re: Re: Fwd: subject".
func NormalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	for subjectPrefixRe.MatchString(subject) {
		subject = subjectPrefixRe.ReplaceAllString(subject, "")
		subject = strings.TrimSpace(subject)
	}
	return subject
}
