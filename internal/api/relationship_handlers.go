package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/darshan-kheni/regent/internal/behavior"
	"github.com/darshan-kheni/regent/internal/middleware"
)

// RelationshipHandlers provides HTTP handlers for relationship intelligence.
type RelationshipHandlers struct {
	svc *behavior.BehaviorService
}

// NewRelationshipHandlers creates a new RelationshipHandlers instance.
func NewRelationshipHandlers(svc *behavior.BehaviorService) *RelationshipHandlers {
	return &RelationshipHandlers{svc: svc}
}

// HandleListRelationships returns paginated, sortable contact relationships.
// GET /api/v1/intelligence/relationships?sort_by=interaction_count|response_time|last_interaction&limit=20&offset=0
func (h *RelationshipHandlers) HandleListRelationships(w http.ResponseWriter, r *http.Request) {
	tc, ok := middleware.GetTenantContext(r.Context())
	if !ok {
		WriteError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return
	}

	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "interaction_count"
	}
	allowedSorts := map[string]bool{
		"interaction_count": true,
		"response_time":    true,
		"last_interaction":  true,
	}
	if !allowedSorts[sortBy] {
		WriteError(w, r, http.StatusBadRequest, "INVALID_PARAM", "sort_by must be interaction_count, response_time, or last_interaction")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	contacts, total, err := h.svc.GetContactRelationships(tc, tc.UserID, sortBy, limit, offset)
	if err != nil {
		slog.Error("behavior: failed to get relationships", "user_id", tc.UserID, "error", err)
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get relationships")
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]interface{}{
		"contacts": contacts,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}
