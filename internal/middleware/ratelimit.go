package middleware

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens    float64
	lastFill  time.Time
	maxTokens float64
	fillRate  float64 // tokens per second
}

func (b *bucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * b.fillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastFill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rpm     int
}

func newRateLimiter(rpm int) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		rpm:     rpm,
	}

	// Clean up old buckets every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for ip, b := range rl.buckets {
		if b.lastFill.Before(cutoff) {
			delete(rl.buckets, ip)
		}
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.buckets[ip]
	if !exists {
		b = &bucket{
			tokens:    float64(rl.rpm),
			lastFill:  time.Now(),
			maxTokens: float64(rl.rpm),
			fillRate:  float64(rl.rpm) / 60.0,
		}
		rl.buckets[ip] = b
	}
	return b.allow()
}

// NewRateLimiter returns a middleware that enforces rate limiting using a
// per-IP token bucket. Returns 429 with Retry-After header when exceeded.
func NewRateLimiter(requestsPerMinute int) func(http.Handler) http.Handler {
	rl := newRateLimiter(requestsPerMinute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, `{"error":"rate limit exceeded","code":"RATE_LIMITED","request_id":"`+GetRequestID(r.Context())+`"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractIP(r *http.Request) string {
	// Use X-Real-IP if set by reverse proxy
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ResetForTesting resets a rate limiter bucket for the given IP. Test use only.
func (rl *rateLimiter) ResetForTesting(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, ip)
}

// NewRateLimiterForTesting creates a rate limiter and returns it alongside the middleware
// for test access.
func NewRateLimiterForTesting(requestsPerMinute int) (*rateLimiter, func(http.Handler) http.Handler) {
	rl := newRateLimiter(requestsPerMinute)
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, fmt.Sprintf(`{"error":"rate limit exceeded","code":"RATE_LIMITED","request_id":"%s"}`, GetRequestID(r.Context())), http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	return rl, mw
}
