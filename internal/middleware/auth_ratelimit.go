package middleware

import (
	"net/http"
	"sync"
	"time"
)

// AuthRateLimiter implements a sliding-window rate limiter for auth endpoints.
// Separate from the global token-bucket rate limiter — this uses a sliding window
// for more precise control over auth-specific rate limits (5/min login, 3/min signup).
type AuthRateLimiter struct {
	mu       sync.Mutex
	windows  map[string]*authWindow
	limit    int
	duration time.Duration
}

type authWindow struct {
	attempts []time.Time
}

// NewAuthRateLimiter creates a sliding-window rate limiter.
// limit is the max requests allowed within duration per IP.
func NewAuthRateLimiter(limit int, dur time.Duration) *AuthRateLimiter {
	rl := &AuthRateLimiter{
		windows:  make(map[string]*authWindow),
		limit:    limit,
		duration: dur,
	}
	go func() {
		for range time.Tick(5 * time.Minute) {
			rl.cleanup()
		}
	}()
	return rl
}

// NewAuthRateLimiterForTesting creates a rate limiter without the cleanup goroutine.
func NewAuthRateLimiterForTesting(limit int, dur time.Duration) *AuthRateLimiter {
	return &AuthRateLimiter{
		windows:  make(map[string]*authWindow),
		limit:    limit,
		duration: dur,
	}
}

// Handler returns middleware that enforces the sliding-window rate limit.
func (rl *AuthRateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr

		rl.mu.Lock()
		win, ok := rl.windows[ip]
		if !ok {
			win = &authWindow{}
			rl.windows[ip] = win
		}

		// Prune attempts outside the window
		cutoff := time.Now().Add(-rl.duration)
		valid := win.attempts[:0]
		for _, t := range win.attempts {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		win.attempts = valid

		if len(win.attempts) >= rl.limit {
			rl.mu.Unlock()
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate limit exceeded","code":"RATE_LIMITED"}`, http.StatusTooManyRequests)
			return
		}

		win.attempts = append(win.attempts, time.Now())
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

func (rl *AuthRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.duration)
	for ip, win := range rl.windows {
		valid := win.attempts[:0]
		for _, t := range win.attempts {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.windows, ip)
		} else {
			win.attempts = valid
		}
	}
}
