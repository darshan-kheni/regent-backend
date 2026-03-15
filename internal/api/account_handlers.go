package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/email"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// AccountHandlers contains HTTP handlers for email account management.
type AccountHandlers struct {
	pool      *pgxpool.Pool
	credStore *email.CredentialStore
}

// NewAccountHandlers creates a new AccountHandlers instance.
func NewAccountHandlers(pool *pgxpool.Pool, encryptor *crypto.RotatingEncryptor) *AccountHandlers {
	var credStore *email.CredentialStore
	if encryptor != nil {
		credStore = email.NewCredentialStore(encryptor, pool)
	}
	return &AccountHandlers{pool: pool, credStore: credStore}
}

// HandleListAccounts handles GET /api/v1/accounts.
func (h *AccountHandlers) HandleListAccounts(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	rows, err := conn.Query(tc,
		`SELECT id, provider, email_address, COALESCE(display_name, ''), sync_status,
		        last_sync_at, created_at, COALESCE(imap_host, ''), COALESCE(imap_port, 993)
		 FROM user_accounts
		 WHERE user_id = $1
		 ORDER BY created_at DESC`, tc.UserID)
	if err != nil {
		slog.Error("query accounts", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list accounts")
		return
	}
	defer rows.Close()

	type accountResponse struct {
		ID           uuid.UUID  `json:"id"`
		Provider     string     `json:"provider"`
		EmailAddress string     `json:"email_address"`
		DisplayName  string     `json:"display_name"`
		SyncStatus   string     `json:"sync_status"`
		LastSyncAt   *string    `json:"last_sync_at"`
		CreatedAt    string     `json:"created_at"`
		IMAPHost     string     `json:"imap_host,omitempty"`
		IMAPPort     int        `json:"imap_port,omitempty"`
	}

	var accounts []accountResponse
	for rows.Next() {
		var a accountResponse
		var lastSync *time.Time
		var createdAt time.Time
		err := rows.Scan(&a.ID, &a.Provider, &a.EmailAddress, &a.DisplayName,
			&a.SyncStatus, &lastSync, &createdAt, &a.IMAPHost, &a.IMAPPort)
		if err != nil {
			slog.Error("scan account", "error", err)
			continue
		}
		a.CreatedAt = createdAt.Format(time.RFC3339)
		if lastSync != nil {
			s := lastSync.Format(time.RFC3339)
			a.LastSyncAt = &s
		}
		accounts = append(accounts, a)
	}

	if accounts == nil {
		accounts = []accountResponse{}
	}

	WriteJSON(w, r, http.StatusOK, accounts)
}

// HandleConnectIMAP handles POST /api/v1/accounts — adds an IMAP account with email + app password.
func (h *AccountHandlers) HandleConnectIMAP(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	if h.credStore == nil {
		WriteError(w, r, http.StatusInternalServerError, "ENCRYPTION_NOT_CONFIGURED", "encryption master key not set")
		return
	}

	var req struct {
		EmailAddress string `json:"email_address"`
		Password     string `json:"password"`
		DisplayName  string `json:"display_name"`
		IMAPHost     string `json:"imap_host"`
		IMAPPort     int    `json:"imap_port"`
		SMTPHost     string `json:"smtp_host"`
		SMTPPort     int    `json:"smtp_port"`
		Provider     string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.EmailAddress == "" || req.Password == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "email_address and password are required")
		return
	}

	// IMAP app password connections always use provider "imap".
	// "gmail" provider is reserved for Gmail API (OAuth) connections.
	req.Provider = "imap"

	// Auto-detect IMAP/SMTP settings from email domain
	if req.IMAPHost == "" {
		req.IMAPHost, req.IMAPPort, req.SMTPHost, req.SMTPPort = detectMailSettings(req.EmailAddress)
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = 993
	}
	if req.SMTPPort == 0 {
		req.SMTPPort = 587
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Check if account already exists
	var existingID uuid.UUID
	err = conn.QueryRow(tc,
		`SELECT id FROM user_accounts WHERE user_id = $1 AND email_address = $2`,
		tc.UserID, req.EmailAddress).Scan(&existingID)
	if err == nil {
		WriteError(w, r, http.StatusConflict, "ACCOUNT_EXISTS", "this email account is already connected")
		return
	}

	// Insert account
	accountID := uuid.New()
	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.EmailAddress
	}

	_, err = conn.Exec(tc,
		`INSERT INTO user_accounts (id, user_id, tenant_id, provider, email_address, display_name,
		                            imap_host, imap_port, smtp_host, smtp_port, sync_status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending')`,
		accountID, tc.UserID, tc.TenantID, req.Provider, req.EmailAddress, displayName,
		req.IMAPHost, req.IMAPPort, req.SMTPHost, req.SMTPPort)
	if err != nil {
		slog.Error("insert account", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create account")
		return
	}

	// Store encrypted IMAP password
	if err := h.credStore.StoreCredential(tc, accountID, "imap_password", req.Password); err != nil {
		slog.Error("store credential", "error", err)
		// Rollback the account creation
		_, _ = conn.Exec(tc, `DELETE FROM user_accounts WHERE id = $1`, accountID)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to store credentials")
		return
	}

	slog.Info("imap account connected",
		"account_id", accountID,
		"email", req.EmailAddress,
		"imap_host", req.IMAPHost,
	)

	WriteJSON(w, r, http.StatusCreated, map[string]interface{}{
		"id":            accountID,
		"provider":      req.Provider,
		"email_address": req.EmailAddress,
		"display_name":  displayName,
		"sync_status":   "pending",
		"imap_host":     req.IMAPHost,
		"imap_port":     req.IMAPPort,
	})
}

// HandleDeleteAccount handles DELETE /api/v1/accounts/{id}.
func (h *AccountHandlers) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	accountIDStr := chi.URLParam(r, "id")
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid account id")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		slog.Error("acquire connection", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()
	if err := database.SetRLSContext(tc, conn); err != nil {
		slog.Error("set rls context", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Delete credentials first (if table exists)
	_, _ = conn.Exec(tc, `DELETE FROM email_credentials WHERE account_id = $1`, accountID)

	// Delete the account
	result, err := conn.Exec(tc,
		`DELETE FROM user_accounts WHERE id = $1 AND user_id = $2`,
		accountID, tc.UserID)
	if err != nil {
		slog.Error("delete account", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete account")
		return
	}

	if result.RowsAffected() == 0 {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "account not found")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "account removed"})
}

// detectMailSettings returns default IMAP/SMTP settings based on email domain.
func detectMailSettings(emailAddr string) (imapHost string, imapPort int, smtpHost string, smtpPort int) {
	// Extract domain
	domain := ""
	for i := len(emailAddr) - 1; i >= 0; i-- {
		if emailAddr[i] == '@' {
			domain = emailAddr[i+1:]
			break
		}
	}

	switch domain {
	case "gmail.com", "googlemail.com":
		return "imap.gmail.com", 993, "smtp.gmail.com", 587
	case "outlook.com", "hotmail.com", "live.com":
		return "outlook.office365.com", 993, "smtp.office365.com", 587
	case "yahoo.com", "ymail.com":
		return "imap.mail.yahoo.com", 993, "smtp.mail.yahoo.com", 587
	case "icloud.com", "me.com", "mac.com":
		return "imap.mail.me.com", 993, "smtp.mail.me.com", 587
	case "aol.com":
		return "imap.aol.com", 993, "smtp.aol.com", 587
	case "zoho.com":
		return "imap.zoho.com", 993, "smtp.zoho.com", 587
	default:
		// Generic: try imap.<domain> and smtp.<domain>
		return "imap." + domain, 993, "smtp." + domain, 587
	}
}
