package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RateLimiterConfig holds rate limiting configuration.
type RateLimiterConfig struct {
	// RequestsPerMinute is the maximum number of requests per minute per IP.
	RequestsPerMinute int
	// BurstSize is the maximum burst size allowed.
	BurstSize int
}

// RateLimiter implements a fixed-window rate limiter.
type RateLimiter struct {
	config    RateLimiterConfig
	logger    *zap.Logger
	requests  map[string]*rateEntry
	mu        sync.Mutex
	lastSweep time.Time
}

type rateEntry struct {
	count   int
	resetAt time.Time
}

// Number of clients tracked before the middleware sweeps expired entries.
// Keeps the map bounded when many distinct IPs hit the service.
const sweepThreshold = 10_000

// NewRateLimiter creates a new RateLimiter.
func NewRateLimiter(config RateLimiterConfig, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		config:   config,
		logger:   logger,
		requests: make(map[string]*rateEntry),
	}
}

// Middleware returns the rate limiting middleware handler.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for infrastructure endpoints.
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		ip := getClientIP(r)
		now := time.Now()

		rl.mu.Lock()
		if len(rl.requests) > sweepThreshold && now.Sub(rl.lastSweep) > time.Minute {
			for key, entry := range rl.requests {
				if now.After(entry.resetAt) {
					delete(rl.requests, key)
				}
			}
			rl.lastSweep = now
		}
		entry, ok := rl.requests[ip]
		if !ok || now.After(entry.resetAt) {
			entry = &rateEntry{
				count:   1,
				resetAt: now.Add(time.Minute),
			}
			rl.requests[ip] = entry
		} else {
			entry.count++
		}
		count := entry.count
		resetAt := entry.resetAt
		rl.mu.Unlock()

		limit := rl.config.RequestsPerMinute
		if count > limit {
			rl.logger.Warn("rate limit exceeded", zap.String("ip", ip), zap.Int("count", count))
			w.Header().Set("Retry-After", "60")
			writeJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests")
			return
		}

		// Add rate limit headers.
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(limit-count))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

		next.ServeHTTP(w, r)
	})
}

// getClientIP extracts the client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (leftmost entry is the original
	// client when the proxy chain appends faithfully).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			xff = xff[:idx]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}

	// Check X-Real-IP header.
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr, handling both IPv4 and IPv6 forms.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
