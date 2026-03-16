package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/database"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// SettingsHandlers contains HTTP handlers for user settings.
type SettingsHandlers struct {
	pool *pgxpool.Pool
}

// NewSettingsHandlers creates a new SettingsHandlers instance.
func NewSettingsHandlers(pool *pgxpool.Pool) *SettingsHandlers {
	return &SettingsHandlers{pool: pool}
}

// HandleGetProfile handles GET /api/v1/settings/profile.
func (h *SettingsHandlers) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
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

	var name, email, timezone, language string
	var avatarURL *string
	err = conn.QueryRow(tc,
		`SELECT COALESCE(full_name, ''), email, avatar_url, COALESCE(timezone, 'UTC'), COALESCE(language, 'en')
		 FROM users WHERE id = $1 OR auth_id = $1`, tc.UserID).Scan(&name, &email, &avatarURL, &timezone, &language)
	if err != nil {
		slog.Error("query profile", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load profile")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"name":       name,
		"email":      email,
		"avatar_url": avatarURL,
		"timezone":   timezone,
		"language":   language,
	})
}

// HandleUpdateProfile handles PUT /api/v1/settings/profile.
// Accepts optional fields: name, timezone, language. Only provided (non-nil) fields are updated.
func (h *SettingsHandlers) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Name     *string `json:"name"`
		Timezone *string `json:"timezone"`
		Language *string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	// Build dynamic UPDATE query for provided fields only
	sets := []string{}
	args := []interface{}{}
	idx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("full_name = $%d", idx))
		args = append(args, *req.Name)
		idx++
	}
	if req.Timezone != nil {
		sets = append(sets, fmt.Sprintf("timezone = $%d", idx))
		args = append(args, *req.Timezone)
		idx++
	}
	if req.Language != nil {
		sets = append(sets, fmt.Sprintf("language = $%d", idx))
		args = append(args, *req.Language)
		idx++
	}

	if len(sets) == 0 {
		WriteJSON(w, r, http.StatusOK, map[string]string{"message": "no changes"})
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

	args = append(args, tc.UserID)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(sets, ", "), idx)
	_, err = conn.Exec(tc, query, args...)
	if err != nil {
		slog.Error("update profile", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update profile")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "profile updated"})
}

// HandleGetNotificationPrefs handles GET /api/v1/settings/notification-prefs.
func (h *SettingsHandlers) HandleGetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
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

	// Try to load notification prefs, return defaults if none exist
	var pushEnabled, smsEnabled, whatsappEnabled, signalEnabled, digestEnabled, vipBreaksQuiet bool
	var digestTime, primaryChannel, digestFrequency string
	var quietStart, quietEnd *string
	err = conn.QueryRow(tc,
		`SELECT COALESCE(push_enabled, true), COALESCE(sms_enabled, false),
		        COALESCE(whatsapp_enabled, false), COALESCE(signal_enabled, false),
		        COALESCE(digest_enabled, true), COALESCE(digest_time::text, '07:00'),
		        COALESCE(primary_channel, 'push'), COALESCE(digest_frequency, 'daily'),
		        quiet_start::text, quiet_end::text, COALESCE(vip_breaks_quiet, true)
		 FROM user_notification_prefs WHERE user_id = $1`, tc.UserID).
		Scan(&pushEnabled, &smsEnabled, &whatsappEnabled, &signalEnabled,
			&digestEnabled, &digestTime, &primaryChannel, &digestFrequency,
			&quietStart, &quietEnd, &vipBreaksQuiet)
	if err != nil {
		// No prefs row — return defaults
		pushEnabled = true
		digestEnabled = true
		digestTime = "07:00"
		primaryChannel = "push"
		digestFrequency = "daily"
		vipBreaksQuiet = true
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"push_enabled":     pushEnabled,
		"sms_enabled":      smsEnabled,
		"whatsapp_enabled": whatsappEnabled,
		"signal_enabled":   signalEnabled,
		"digest_enabled":   digestEnabled,
		"digest_time":      digestTime,
		"primary_channel":  primaryChannel,
		"digest_frequency": digestFrequency,
		"quiet_start":      quietStart,
		"quiet_end":        quietEnd,
		"vip_breaks_quiet": vipBreaksQuiet,
	})
}

// HandleUpdateNotificationPrefs handles PUT /api/v1/settings/notification-prefs.
func (h *SettingsHandlers) HandleUpdateNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		PushEnabled     *bool   `json:"push_enabled"`
		SmsEnabled      *bool   `json:"sms_enabled"`
		WhatsappEnabled *bool   `json:"whatsapp_enabled"`
		SignalEnabled   *bool   `json:"signal_enabled"`
		DigestEnabled   *bool   `json:"digest_enabled"`
		DigestTime      *string `json:"digest_time"`
		PrimaryChannel  *string `json:"primary_channel"`
		DigestFrequency *string `json:"digest_frequency"`
		VipBreaksQuiet  *bool   `json:"vip_breaks_quiet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
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

	// Upsert notification prefs
	_, err = conn.Exec(tc, `
		INSERT INTO user_notification_prefs (user_id, tenant_id, push_enabled, sms_enabled, whatsapp_enabled, signal_enabled, digest_enabled, digest_time, primary_channel, digest_frequency, vip_breaks_quiet)
		VALUES ($1, $2,
			COALESCE($3, true), COALESCE($4, false), COALESCE($5, false), COALESCE($6, false),
			COALESCE($7, true), COALESCE($8::time, '07:00'), COALESCE($9, 'push'),
			COALESCE($10, 'daily'), COALESCE($11, true))
		ON CONFLICT (user_id) DO UPDATE SET
			push_enabled = COALESCE($3, user_notification_prefs.push_enabled),
			sms_enabled = COALESCE($4, user_notification_prefs.sms_enabled),
			whatsapp_enabled = COALESCE($5, user_notification_prefs.whatsapp_enabled),
			signal_enabled = COALESCE($6, user_notification_prefs.signal_enabled),
			digest_enabled = COALESCE($7, user_notification_prefs.digest_enabled),
			digest_time = COALESCE($8::time, user_notification_prefs.digest_time),
			primary_channel = COALESCE($9, user_notification_prefs.primary_channel),
			digest_frequency = COALESCE($10, user_notification_prefs.digest_frequency),
			vip_breaks_quiet = COALESCE($11, user_notification_prefs.vip_breaks_quiet),
			updated_at = NOW()`,
		tc.UserID, tc.TenantID, req.PushEnabled, req.SmsEnabled, req.WhatsappEnabled, req.SignalEnabled,
		req.DigestEnabled, req.DigestTime, req.PrimaryChannel, req.DigestFrequency, req.VipBreaksQuiet)
	if err != nil {
		slog.Error("upsert notification prefs", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update notification preferences")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "notification preferences updated"})
}

// HandleGetAiPrefs handles GET /api/v1/settings/ai-prefs.
func (h *SettingsHandlers) HandleGetAiPrefs(w http.ResponseWriter, r *http.Request) {
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

	var formality int
	var replyStyle string
	err = conn.QueryRow(tc,
		`SELECT COALESCE(formality, 3), COALESCE(reply_style, 'professional') FROM users WHERE id = $1`,
		tc.UserID).Scan(&formality, &replyStyle)
	if err != nil {
		slog.Error("query ai prefs", "error", err)
		// Return defaults on error
		WriteJSON(w, r, http.StatusOK, map[string]interface{}{
			"formality":   3,
			"reply_style": "professional",
		})
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"formality":   formality,
		"reply_style": replyStyle,
	})
}

// HandleUpdateAiPrefs handles PUT /api/v1/settings/ai-prefs.
func (h *SettingsHandlers) HandleUpdateAiPrefs(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Formality  *int    `json:"formality"`
		ReplyStyle *string `json:"reply_style"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	// Build dynamic update for provided fields only
	sets := []string{}
	args := []interface{}{}
	idx := 1

	if req.Formality != nil {
		sets = append(sets, fmt.Sprintf("formality = $%d", idx))
		args = append(args, *req.Formality)
		idx++
	}
	if req.ReplyStyle != nil {
		sets = append(sets, fmt.Sprintf("reply_style = $%d", idx))
		args = append(args, *req.ReplyStyle)
		idx++
	}

	if len(sets) == 0 {
		WriteJSON(w, r, http.StatusOK, map[string]string{"message": "no changes"})
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

	args = append(args, tc.UserID)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(sets, ", "), idx)
	_, err = conn.Exec(tc, query, args...)
	if err != nil {
		slog.Error("update ai prefs", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update preferences")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "preferences updated"})
}
