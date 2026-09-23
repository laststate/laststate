package queue

import (
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewFromEnv selects a Jober backend.
// TRACE_QUEUE=postgres|memory|nats|redis|cf
func NewFromEnv(pool *pgxpool.Pool, driver, brokerURL string) (Jober, string, error) {
	d := strings.ToLower(strings.TrimSpace(driver))
	switch d {
	case "", "postgres", "pg":
		if pool == nil {
			return nil, "", fmt.Errorf("postgres queue requires pool")
		}
		return &Queue{Pool: pool}, "postgres", nil
	case "memory":
		return NewMemory(), "memory", nil
	case "nats":
		// Explicit in-memory JetStream-shaped simulator. Not a network NATS client.
		// Name is honest so operators do not assume durability across restarts.
		// For production durable queues use TRACE_QUEUE=postgres or redis.
		u := strings.ToLower(strings.TrimSpace(brokerURL))
		if u != "" && u != "memory" && u != "local" && !strings.HasPrefix(u, "memory:") && !strings.HasPrefix(u, "local:") {
			return nil, "", fmt.Errorf("TRACE_QUEUE=nats is an in-process memory simulator only (set TRACE_QUEUE_URL=memory or empty); use postgres/redis for durable multi-node")
		}
		return NewNATSJetStreamLocal(), "nats-memory-sim", nil
	case "redis":
		if brokerURL == "" {
			brokerURL = "127.0.0.1:6379"
		}
		// strip redis://
		addr := strings.TrimPrefix(brokerURL, "redis://")
		addr = strings.TrimPrefix(addr, "rediss://")
		if i := strings.Index(addr, "/"); i >= 0 {
			addr = addr[:i]
		}
		// Use TRACE_QUEUE_CHANNEL for the Redis channel name, defaulting to "trace:jobs".
		channel := os.Getenv("TRACE_QUEUE_CHANNEL")
		if channel == "" {
			channel = "trace:jobs"
		}
		return NewRedis(addr, channel), "redis", nil
	case "cf", "cloudflare":
		// TRACE_QUEUE_URL = CF messages API base; optional token after |
		// e.g. https://api.cloudflare.com/.../messages|token
		base, token := brokerURL, ""
		if i := strings.Index(brokerURL, "|"); i >= 0 {
			base, token = brokerURL[:i], brokerURL[i+1:]
		}
		if base == "" {
			return NewCFLocal(), "cf-local", nil
		}
		return NewCFHTTP(base, token), "cf-http", nil
	default:
		return nil, "", fmt.Errorf("unknown TRACE_QUEUE=%s", driver)
	}
}
