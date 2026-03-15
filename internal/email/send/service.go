package send

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/email"
)

// SendRequest contains everything needed to send an email.
type SendRequest struct {
	AccountID   uuid.UUID
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Body        string
	InReplyTo   string    // Message-ID of email being replied to
	ThreadID    *uuid.UUID // Thread ID for replies
}

// Service handles sending emails via SMTP and recording them.
type Service struct {
	pool      *pgxpool.Pool
	credStore *email.CredentialStore
}

// NewService creates a new send service.
func NewService(pool *pgxpool.Pool, encryptor *crypto.RotatingEncryptor) *Service {
	var credStore *email.CredentialStore
	if encryptor != nil {
		credStore = email.NewCredentialStore(encryptor, pool)
	}
	return &Service{pool: pool, credStore: credStore}
}

// Send sends an email and records it as an outbound email in the database.
func (s *Service) Send(ctx database.TenantContext, req SendRequest) (uuid.UUID, error) {
	// 1. Load account details
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return uuid.Nil, fmt.Errorf("setting RLS: %w", err)
	}

	var fromEmail, fromName, smtpHost string
	var smtpPort int
	var provider string
	err = conn.QueryRow(ctx,
		`SELECT email_address, COALESCE(display_name, ''), COALESCE(smtp_host, ''), COALESCE(smtp_port, 587), provider
		 FROM user_accounts WHERE id = $1 AND user_id = $2`,
		req.AccountID, ctx.UserID,
	).Scan(&fromEmail, &fromName, &smtpHost, &smtpPort, &provider)
	if err != nil {
		return uuid.Nil, fmt.Errorf("loading account: %w", err)
	}

	// 2. Get SMTP credentials
	if s.credStore == nil {
		return uuid.Nil, fmt.Errorf("credential store not configured")
	}

	var auth smtp.Auth
	if provider == "gmail" {
		// Gmail uses OAuth2 token for SMTP
		token, err := s.credStore.GetCredential(ctx, req.AccountID, "access_token")
		if err != nil {
			return uuid.Nil, fmt.Errorf("getting OAuth token: %w", err)
		}
		auth = NewXOAuth2Auth(fromEmail, token)
		if smtpHost == "" {
			smtpHost = "smtp.gmail.com"
			smtpPort = 587
		}
	} else {
		// IMAP/SMTP accounts use password
		password, err := s.credStore.GetCredential(ctx, req.AccountID, "smtp_password")
		if err != nil {
			// Try IMAP password as fallback
			password, err = s.credStore.GetCredential(ctx, req.AccountID, "imap_password")
			if err != nil {
				return uuid.Nil, fmt.Errorf("getting SMTP password: %w", err)
			}
		}
		auth = smtp.PlainAuth("", fromEmail, password, smtpHost)
	}

	// 3. Build RFC 2822 message
	msgID := fmt.Sprintf("<%s@regent.ai>", uuid.New().String())
	allRecipients := append(append([]string{}, req.To...), req.Cc...)
	allRecipients = append(allRecipients, req.Bcc...)

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, fromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(req.To, ", ")))
	if len(req.Cc) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(req.Cc, ", ")))
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", req.Subject))
	msg.WriteString(fmt.Sprintf("Message-ID: %s\r\n", msgID))
	if req.InReplyTo != "" {
		msg.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", req.InReplyTo))
		msg.WriteString(fmt.Sprintf("References: %s\r\n", req.InReplyTo))
	}
	msg.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(req.Body)

	// 4. Send via SMTP
	err = SendViaSMTP(smtpHost, smtpPort, auth, fromEmail, allRecipients, []byte(msg.String()))
	if err != nil {
		slog.Error("SMTP send failed", "error", err, "to", req.To, "account", fromEmail)
		// Still record the attempt
		s.recordSent(ctx, conn, req, fromEmail, fromName, msgID, "failed")
		return uuid.Nil, fmt.Errorf("sending email: %w", err)
	}

	slog.Info("email sent", "from", fromEmail, "to", req.To, "subject", req.Subject)

	// 5. Record in emails table as outbound
	emailID := s.recordSent(ctx, conn, req, fromEmail, fromName, msgID, "sent")

	return emailID, nil
}

// recordSent inserts the outbound email into the emails table.
func (s *Service) recordSent(ctx database.TenantContext, conn *pgxpool.Conn, req SendRequest, fromEmail, fromName, msgID, status string) uuid.UUID {
	toJSON, _ := json.Marshal(req.To)
	ccJSON, _ := json.Marshal(req.Cc)

	emailID := uuid.New()
	now := time.Now()
	// Generate a unique UID for outbound emails (negative to avoid IMAP UID collision)
	uid := -now.UnixNano() / 1000000

	_, err := conn.Exec(ctx,
		`INSERT INTO emails (id, tenant_id, user_id, account_id, message_id, uid, direction,
		                      from_address, from_name, to_addresses, cc_addresses,
		                      subject, body_text, has_attachments, received_at, is_read,
		                      in_reply_to, thread_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'outbound', $7, $8, $9, $10, $11, $12, false, $13, true, $14, $15, $13)`,
		emailID, ctx.TenantID, ctx.UserID, req.AccountID, msgID, uid,
		fromEmail, fromName, toJSON, ccJSON,
		req.Subject, req.Body, now, req.InReplyTo, req.ThreadID,
	)
	if err != nil {
		slog.Error("recording sent email", "error", err)
		return uuid.Nil
	}

	// Also log in email_send_log
	_, _ = conn.Exec(ctx,
		`INSERT INTO email_send_log (tenant_id, user_id, account_id, email_id, to_addresses, subject, method, status, sent_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		ctx.TenantID, ctx.UserID, req.AccountID, emailID, toJSON,
		req.Subject, "smtp", status, now,
	)

	return emailID
}
