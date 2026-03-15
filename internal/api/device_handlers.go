package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/auth"
	"github.com/darshan-kheni/regent/internal/briefings"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// DeviceHandlers contains HTTP handlers for device token management.
type DeviceHandlers struct {
	pool *pgxpool.Pool
}

// NewDeviceHandlers creates a new DeviceHandlers instance.
func NewDeviceHandlers(pool *pgxpool.Pool) *DeviceHandlers {
	return &DeviceHandlers{pool: pool}
}

// RegisterDevice handles POST /api/v1/devices/register.
// Registers or updates a device token for push notifications.
func (h *DeviceHandlers) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		Unauthorized(w, r, "missing tenant context")
		return
	}

	var req struct {
		Token      string `json:"token"`
		Platform   string `json:"platform"`
		DeviceName string `json:"device_name"`
		AppVersion string `json:"app_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Token == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "token is required")
		return
	}

	// Validate platform
	switch req.Platform {
	case "android", "ios", "web":
		// valid
	default:
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "platform must be android, ios, or web")
		return
	}

	err := briefings.RegisterDeviceToken(r.Context(), h.pool,
		user.ID, tc.TenantID, req.Token, req.Platform, req.DeviceName, req.AppVersion)
	if err != nil {
		slog.Error("register device token failed", "error", err, "user_id", user.ID)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to register device")
		return
	}

	slog.Info("device token registered", "user_id", user.ID, "platform", req.Platform)

	WriteJSON(w, r, http.StatusCreated, map[string]interface{}{
		"registered": true,
		"platform":   req.Platform,
	})
}

// DeregisterDevice handles DELETE /api/v1/devices/{token}.
// Removes a device token.
func (h *DeviceHandlers) DeregisterDevice(w http.ResponseWriter, r *http.Request) {
	_ = auth.GetUser(r.Context()) // Ensure authenticated

	token := chi.URLParam(r, "token")
	if token == "" {
		WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "token is required")
		return
	}

	err := briefings.DeregisterDeviceToken(r.Context(), h.pool, token)
	if err != nil {
		slog.Error("deregister device token failed", "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to deregister device")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]string{"message": "device deregistered"})
}
