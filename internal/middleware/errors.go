package middleware

import (
	"encoding/json"
	"net/http"
	"time"
)

// writeJSONError writes a standard JSON error response from middleware.
// This avoids importing the api package (which imports middleware → import cycle).
func writeJSONError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	resp := struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
		Timestamp string `json:"timestamp"`
	}{
		Error:     message,
		Code:      code,
		RequestID: GetRequestID(r.Context()),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
