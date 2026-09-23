// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package admin

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig controls the token-bucket rate limiter.
type RateLimitConfig struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	BurstSize         int     `json:"burst_size"`
	MaxKeys           int     `json:"max_keys"`
}

// NewRateLimiter creates a per-IP token-bucket rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = 50
	}
	if config.BurstSize <= 0 {
		config.BurstSize = int(config.RequestsPerSecond)
	}
	if config.MaxKeys <= 0 {
		config.MaxKeys = 10000
	}
	return &RateLimiter{
		config:    config,
		buckets:   make(map[string]*tokenBucket),
		mu:        sync.Mutex{},
		lastClean: time.Now(),
	}
}

// RateLimiter enforces per-IP token-bucket rate limiting.
type RateLimiter struct {
	config    RateLimitConfig
	buckets   map[string]*tokenBucket
	mu        sync.Mutex
	lastClean time.Time
}

type tokenBucket struct {
	tokens    float64
	lastReset time.Time
	count     int
}

// Allow checks whether the given IP is permitted to proceed.
func (rl *RateLimiter) Allow(ip string) (bool, RateLimitHeaders) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if time.Since(rl.lastClean) > 5*time.Minute {
		rl.prune()
		rl.lastClean = time.Now()
	}

	bucket, ok := rl.buckets[ip]
	if !ok {
		bucket = &tokenBucket{
			tokens:    float64(rl.config.BurstSize),
			lastReset: time.Now(),
		}
		if len(rl.buckets) < rl.config.MaxKeys {
			rl.buckets[ip] = bucket
		}
	}

	elapsed := time.Since(bucket.lastReset).Seconds()
	bucket.tokens += elapsed * rl.config.RequestsPerSecond
	if bucket.tokens > float64(rl.config.BurstSize) {
		bucket.tokens = float64(rl.config.BurstSize)
	}
	bucket.lastReset = time.Now()
	bucket.count++

	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true, RateLimitHeaders{
			Limit:     rl.config.BurstSize,
			Remaining: int(bucket.tokens),
			Reset:     time.Now().Add(time.Second).Unix(),
		}
	}

	return false, RateLimitHeaders{
		Limit:     rl.config.BurstSize,
		Remaining: 0,
		Reset:     time.Now().Add(time.Duration(float64(time.Second) * ((1.0 - bucket.tokens) / rl.config.RequestsPerSecond))).Unix(),
	}
}

// prune removes stale buckets.
func (rl *RateLimiter) prune() {
	cutoff := time.Now().Add(-10 * time.Minute)
	for ip, bucket := range rl.buckets {
		if bucket.lastReset.Before(cutoff) {
			delete(rl.buckets, ip)
		}
	}
}

// RateLimitHeaders are returned in response headers.
type RateLimitHeaders struct {
	Limit     int
	Remaining int
	Reset     int64
}

// RateLimitMiddleware wraps the next handler with per-IP rate limiting.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ip := extractIP(request)
			allowed, headers := rl.Allow(ip)

			writer.Header().Set("X-RateLimit-Limit", strconv.Itoa(headers.Limit))
			writer.Header().Set("X-RateLimit-Remaining", strconv.Itoa(headers.Remaining))
			writer.Header().Set("X-RateLimit-Reset", strconv.FormatInt(headers.Reset, 10))

			if !allowed {
				writer.Header().Set("Retry-After", "1")
				writeError(writer, http.StatusTooManyRequests, "rate_limit_exceeded", "too many requests")
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

// extractIP extracts the client IP from the request.
func extractIP(request *http.Request) string {
	if xff := request.Header.Get("X-Forwarded-For"); xff != "" {
		for _, ip := range strings.Split(xff, ",") {
			ip = strings.TrimSpace(ip)
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if xri := request.Header.Get("X-Real-IP"); xri != "" {
		if ip := net.ParseIP(strings.TrimSpace(xri)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}
