package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/email/send"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// SendHandlers contains HTTP handlers for sending emails.
type SendHandlers struct {
	pool    *pgxpool.Pool
	sender  *send.Service
}

// NewSendHandlers creates a new SendHandlers instance.
func NewSendHandlers(pool *pgxpool.Pool, sender *send.Service) *SendHandlers {
	return &SendHandlers{pool: pool, sender: sender}
}

// HandleComposeSend handles POST /api/v1/compose/send.
func (h *SendHandlers) HandleComposeSend(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		AccountID   string   `json:"account_id"`
		To          []string `json:"to_addresses"`
		Cc          []string `json:"cc_addresses"`
		Bcc         []string `json:"bcc_addresses"`
		Subject     string   `json:"subject"`
		Body        string   `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid account_id")
		return
	}

	if len(req.To) == 0 {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "at least one recipient required")
		return
	}

	emailID, err := h.sender.Send(tc, send.SendRequest{
		AccountID: accountID,
		To:        req.To,
		Cc:        req.Cc,
		Bcc:       req.Bcc,
		Subject:   req.Subject,
		Body:      req.Body,
	})
	if err != nil {
		slog.Error("compose send failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "SEND_FAILED", "Failed to send email: "+err.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"status":   "sent",
		"email_id": emailID,
		"message":  "Email sent successfully",
	})
}

// HandleApproveDraftAndSend handles POST /api/v1/drafts/{id}/approve.
// It sends the draft reply via SMTP and records it as an outbound email.
func (h *SendHandlers) HandleApproveDraftAndSend(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid draft id")
		return
	}

	conn, err := h.pool.Acquire(tc)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}
	defer conn.Release()

	if err := database.SetRLSContext(tc, conn); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "database error")
		return
	}

	// Load draft + original email in one query
	var draftBody string
	var emailSubject, emailFromAddress, emailMessageID string
	var accountID uuid.UUID
	var threadID *uuid.UUID

	err = conn.QueryRow(tc,
		`SELECT d.body, e.subject, e.from_address, COALESCE(e.message_id, ''), e.account_id, e.thread_id
		 FROM draft_replies d
		 JOIN emails e ON e.id = d.email_id
		 WHERE d.id = $1 AND d.user_id = $2 AND d.status = 'pending'`,
		draftID, tc.UserID,
	).Scan(&draftBody, &emailSubject, &emailFromAddress, &emailMessageID, &accountID, &threadID)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "draft not found or already processed")
		return
	}

	// Build reply subject
	replySubject := emailSubject
	if len(replySubject) < 4 || replySubject[:4] != "Re: " {
		replySubject = "Re: " + replySubject
	}

	// Send the reply
	emailID, sendErr := h.sender.Send(tc, send.SendRequest{
		AccountID: accountID,
		To:        []string{emailFromAddress},
		Subject:   replySubject,
		Body:      draftBody,
		InReplyTo: emailMessageID,
		ThreadID:  threadID,
	})

	// Update draft status regardless
	newStatus := "sent"
	if sendErr != nil {
		newStatus = "approved" // Mark approved even if send fails (they can retry)
		slog.Error("draft send failed", "draft_id", draftID, "error", sendErr)
	}

	_, _ = conn.Exec(tc,
		`UPDATE draft_replies SET status = $1 WHERE id = $2 AND user_id = $3`,
		newStatus, draftID, tc.UserID,
	)

	if sendErr != nil {
		WriteError(w, r, http.StatusInternalServerError, "SEND_FAILED", "Draft approved but send failed: "+sendErr.Error())
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"status":   "sent",
		"email_id": emailID,
	})
}
