package queue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/auth"
)

// RedisQueue implements Jober with Redis LIST + HASH (minimal RESP, no deps).
// Keys: {prefix}:ready (list), {prefix}:job:{id} (hash fields).
type RedisQueue struct {
	Addr   string // host:port
	Prefix string
	DB     int

	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
}

func NewRedis(addr, prefix string) *RedisQueue {
	if prefix == "" {
		prefix = "trace:jobs"
	}
	return &RedisQueue{Addr: addr, Prefix: prefix}
}

func (q *RedisQueue) dial() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.conn != nil {
		return nil
	}
	c, err := net.DialTimeout("tcp", q.Addr, 5*time.Second)
	if err != nil {
		return err
	}
	q.conn = c
	q.rd = bufio.NewReader(c)
	if q.DB > 0 {
		if err := q.cmd("SELECT", strconv.Itoa(q.DB)); err != nil {
			_ = c.Close()
			q.conn = nil
			return err
		}
		_, _ = q.readReply()
	}
	return nil
}

func (q *RedisQueue) cmd(parts ...string) error {
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(strconv.Itoa(len(parts)))
	b.WriteString("\r\n")
	for _, p := range parts {
		b.WriteString("$")
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteString("\r\n")
		b.WriteString(p)
		b.WriteString("\r\n")
	}
	_, err := q.conn.Write([]byte(b.String()))
	return err
}

func (q *RedisQueue) readReply() (string, error) {
	line, err := q.rd.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 1 {
		return "", fmt.Errorf("empty redis reply")
	}
	switch line[0] {
	case '+', ':':
		return strings.TrimSpace(line[1:]), nil
	case '-':
		return "", fmt.Errorf("redis error: %s", strings.TrimSpace(line[1:]))
	case '$':
		n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
		if n < 0 {
			return "", nil
		}
		buf := make([]byte, n+2)
		if _, err := q.rd.Read(buf); err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	case '*':
		n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
		var parts []string
		for i := 0; i < n; i++ {
			s, err := q.readReply()
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n"), nil
	default:
		return strings.TrimSpace(line), nil
	}
}

func (q *RedisQueue) withConn(fn func() error) error {
	if err := q.dial(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	err := fn()
	if err != nil {
		_ = q.conn.Close()
		q.conn = nil
		q.rd = nil
	}
	return err
}

func (q *RedisQueue) Enqueue(ctx context.Context, typ string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	id := uuid.New()
	key := q.Prefix + ":job:" + id.String()
	return q.withConn(func() error {
		if err := q.cmd("HSET", key, "type", typ, "payload", string(raw), "attempts", "0", "status", "ready"); err != nil {
			return err
		}
		if _, err := q.readReply(); err != nil {
			return err
		}
		if err := q.cmd("LPUSH", q.Prefix+":ready", id.String()); err != nil {
			return err
		}
		_, err := q.readReply()
		return err
	})
}

func (q *RedisQueue) Claim(ctx context.Context, leaseFor time.Duration) (Job, error) {
	var out Job
	err := q.withConn(func() error {
		// Reclaim expired leases from ZSET score=until before BRPOP
		now := time.Now().Unix()
		if err := q.cmd("ZRANGEBYSCORE", q.Prefix+":leased", "-inf", strconv.FormatInt(now, 10), "LIMIT", "0", "32"); err == nil {
			if reply, err := q.readReply(); err == nil && reply != "" {
				for _, idStr := range strings.Split(reply, "\n") {
					idStr = strings.TrimSpace(idStr)
					if idStr == "" {
						continue
					}
					_ = q.cmd("ZREM", q.Prefix+":leased", idStr)
					_, _ = q.readReply()
					key := q.Prefix + ":job:" + idStr
					_ = q.cmd("HSET", key, "status", "ready", "lease", "")
					_, _ = q.readReply()
					_ = q.cmd("LPUSH", q.Prefix+":ready", idStr)
					_, _ = q.readReply()
				}
			}
		}

		// BRPOP 1 second
		if err := q.cmd("BRPOP", q.Prefix+":ready", "1"); err != nil {
			return err
		}
		reply, err := q.readReply()
		if err != nil {
			return err
		}
		if reply == "" {
			return pgx.ErrNoRows
		}
		// reply is key\nid
		parts := strings.Split(reply, "\n")
		idStr := parts[len(parts)-1]
		id, err := uuid.Parse(idStr)
		if err != nil {
			return err
		}
		key := q.Prefix + ":job:" + id.String()
		if err := q.cmd("HGETALL", key); err != nil {
			return err
		}
		all, err := q.readReply()
		if err != nil {
			return err
		}
		fields := parseHGetAll(all)
		if fields["type"] == "" {
			return pgx.ErrNoRows
		}
		lease := auth.RandomHex(12)
		attempts, _ := strconv.Atoi(fields["attempts"])
		attempts++
		until := time.Now().Add(leaseFor).Unix()
		if err := q.cmd("HSET", key, "status", "leased", "lease", lease, "attempts", strconv.Itoa(attempts), "until", strconv.FormatInt(until, 10)); err != nil {
			return err
		}
		if _, err := q.readReply(); err != nil {
			return err
		}
		// track lease expiry for reclaim
		_ = q.cmd("ZADD", q.Prefix+":leased", strconv.FormatInt(until, 10), id.String())
		_, _ = q.readReply()
		out = Job{ID: id, Type: fields["type"], Payload: json.RawMessage(fields["payload"]), Attempt: attempts, Lease: lease}
		return nil
	})
	return out, err
}

func parseHGetAll(s string) map[string]string {
	lines := strings.Split(s, "\n")
	m := map[string]string{}
	for i := 0; i+1 < len(lines); i += 2 {
		m[lines[i]] = lines[i+1]
	}
	return m
}

func (q *RedisQueue) Renew(ctx context.Context, id uuid.UUID, lease string, leaseFor time.Duration) error {
	return q.withConn(func() error {
		key := q.Prefix + ":job:" + id.String()
		if err := q.cmd("HGET", key, "lease"); err != nil {
			return err
		}
		cur, err := q.readReply()
		if err != nil || cur != lease {
			return errLease
		}
		until := time.Now().Add(leaseFor).Unix()
		if err := q.cmd("HSET", key, "until", strconv.FormatInt(until, 10)); err != nil {
			return err
		}
		_, err = q.readReply()
		return err
	})
}

func (q *RedisQueue) Complete(ctx context.Context, id uuid.UUID, lease string) error {
	return q.withConn(func() error {
		key := q.Prefix + ":job:" + id.String()
		if err := q.cmd("HGET", key, "lease"); err != nil {
			return err
		}
		cur, err := q.readReply()
		if err != nil || cur != lease {
			return errLease
		}
		if err := q.cmd("HSET", key, "status", "done"); err != nil {
			return err
		}
		if _, err := q.readReply(); err != nil {
			return err
		}
		_ = q.cmd("ZREM", q.Prefix+":leased", id.String())
		_, _ = q.readReply()
		return nil
	})
}

func (q *RedisQueue) Fail(ctx context.Context, id uuid.UUID, lease, msg string, retryIn time.Duration, maxAttempts int) error {
	return q.withConn(func() error {
		key := q.Prefix + ":job:" + id.String()
		if err := q.cmd("HGET", key, "lease"); err != nil {
			return err
		}
		cur, err := q.readReply()
		if err != nil || cur != lease {
			return errLease
		}
		if err := q.cmd("HGET", key, "attempts"); err != nil {
			return err
		}
		as, _ := q.readReply()
		attempts, _ := strconv.Atoi(as)
		status := "retry"
		if attempts >= maxAttempts {
			status = "dead"
		}
		if err := q.cmd("HSET", key, "status", status, "error", msg, "lease", ""); err != nil {
			return err
		}
		if _, err := q.readReply(); err != nil {
			return err
		}
		if status == "retry" {
			// requeue after delay (best-effort immediate for minimal client)
			time.AfterFunc(retryIn, func() {
				_ = q.withConn(func() error {
					if err := q.cmd("LPUSH", q.Prefix+":ready", id.String()); err != nil {
						return err
					}
					_, err := q.readReply()
					return err
				})
			})
		}
		return nil
	})
}
