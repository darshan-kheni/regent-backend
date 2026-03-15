package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthHandler returns the liveness probe handler.
// Always returns 200 — no dependency checks.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

type checkResult struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
}

// ReadyHandler returns the readiness probe handler.
// Checks DB connectivity. Returns 503 if DB is down.
func ReadyHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := make(map[string]checkResult)

		// Database check
		dbStart := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		dbStatus := "up"
		if err := pool.Ping(ctx); err != nil {
			dbStatus = "down"
		}
		checks["database"] = checkResult{
			Status:    dbStatus,
			LatencyMS: time.Since(dbStart).Milliseconds(),
		}

		// Redis check — not configured in Phase 1
		checks["redis"] = checkResult{
			Status:    "not_configured",
			LatencyMS: 0,
		}

		status := "ready"
		httpStatus := http.StatusOK
		if dbStatus != "up" {
			status = "not_ready"
			httpStatus = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"checks": checks,
		})
	}
}
