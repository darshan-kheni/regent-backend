package api

import (
	"encoding/json"
	"net/http"

	"github.com/darshan-kheni/regent/internal/middleware"
)

// SuccessResponse is the standard JSON success response format.
type SuccessResponse struct {
	Data      interface{} `json:"data"`
	Meta      interface{} `json:"meta,omitempty"`
	RequestID string      `json:"request_id"`
}

// WriteJSON writes a standard success response.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	resp := SuccessResponse{
		Data:      data,
		RequestID: middleware.GetRequestID(r.Context()),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
