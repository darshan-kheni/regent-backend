package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// NewRecoverer returns a middleware that recovers from panics and returns a 500
// response with the correlation request ID.
func NewRecoverer() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqID := GetRequestID(r.Context())
					slog.Error("panic recovered",
						"error", rec,
						"request_id", reqID,
						"stack", string(debug.Stack()),
					)
					http.Error(w, `{"error":"internal server error","code":"INTERNAL_ERROR","request_id":"`+reqID+`"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
