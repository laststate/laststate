package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/auth"
)

// CFHTTPQueue implements Cloudflare Queues-style HTTP pull:
// POST {base}/messages/pull  → claim batch
// POST {base}/messages/ack   → complete
// When baseURL is empty, falls back to in-memory CF local.
//
// This is the production-shaped adapter; set TRACE_QUEUE=cf and
// TRACE_QUEUE_URL=https://api.cloudflare.com/.../queues/.../messages
type CFHTTPQueue struct {
	BaseURL string
	Token   string
	Client  *http.Client

	// local fallback when BaseURL empty or HTTP fails on Enqueue in dev
	local *MemoryQueue
	mu    sync.Mutex
	// map lease -> remote message id
	leases map[string]string
}

func NewCFHTTP(baseURL, token string) *CFHTTPQueue {
	return &CFHTTPQueue{
		BaseURL: baseURL,
		Token:   token,
		Client:  &http.Client{Timeout: 15 * time.Second},
		local:   NewMemory(),
		leases:  map[string]string{},
	}
}

func (q *CFHTTPQueue) useLocal() bool {
	return q.BaseURL == ""
}

func (q *CFHTTPQueue) Enqueue(ctx context.Context, typ string, payload any) error {
	if err := GuardEnqueue(ctx, q); err != nil {
		return err
	}
	if q.useLocal() {
		// Explicit local-only mode when TRACE_QUEUE_URL is empty (dev).
		return q.local.Enqueue(ctx, typ, payload)
	}
	body, _ := json.Marshal(map[string]any{
		"body": map[string]any{"type": typ, "payload": payload},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	q.auth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.Client.Do(req)
	if err != nil {
		// Never fall back to RAM when a remote URL was configured — jobs would vanish.
		return fmt.Errorf("cf enqueue network: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("cf enqueue %s: %s", resp.Status, b)
	}
	return nil
}

func (q *CFHTTPQueue) Claim(ctx context.Context, leaseFor time.Duration) (Job, error) {
	if q.useLocal() {
		return q.local.Claim(ctx, leaseFor)
	}
	body, _ := json.Marshal(map[string]any{
		"batch_size":            1,
		"visibility_timeout_ms": int(leaseFor / time.Millisecond),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.BaseURL+"/messages/pull", bytes.NewReader(body))
	if err != nil {
		return Job{}, err
	}
	q.auth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.Client.Do(req)
	if err != nil {
		return Job{}, fmt.Errorf("cf claim network: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 || resp.StatusCode == 404 {
		return Job{}, pgx.ErrNoRows
	}
	var parsed struct {
		Messages []struct {
			ID   string          `json:"id"`
			Body json.RawMessage `json:"body"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Job{}, err
	}
	if len(parsed.Messages) == 0 {
		return Job{}, pgx.ErrNoRows
	}
	m := parsed.Messages[0]
	var wrap struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	_ = json.Unmarshal(m.Body, &wrap)
	if wrap.Type == "" {
		wrap.Type = "process_event"
		wrap.Payload = m.Body
	}
	lease := auth.RandomHex(12)
	q.mu.Lock()
	q.leases[lease] = m.ID
	q.mu.Unlock()
	return Job{
		ID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(m.ID)),
		Type:    wrap.Type,
		Payload: wrap.Payload,
		Attempt: 1,
		Lease:   lease,
	}, nil
}

func (q *CFHTTPQueue) Renew(ctx context.Context, id uuid.UUID, lease string, leaseFor time.Duration) error {
	if q.useLocal() {
		return q.local.Renew(ctx, id, lease, leaseFor)
	}
	// CF visibility timeout extension not always available — no-op OK
	return nil
}

func (q *CFHTTPQueue) Complete(ctx context.Context, id uuid.UUID, lease string) error {
	if q.useLocal() {
		return q.local.Complete(ctx, id, lease)
	}
	q.mu.Lock()
	mid := q.leases[lease]
	delete(q.leases, lease)
	q.mu.Unlock()
	if mid == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"acks": []map[string]string{{"id": mid}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.BaseURL+"/messages/ack", bytes.NewReader(body))
	if err != nil {
		return err
	}
	q.auth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (q *CFHTTPQueue) Fail(ctx context.Context, id uuid.UUID, lease, msg string, retryIn time.Duration, maxAttempts int) error {
	if q.useLocal() {
		return q.local.Fail(ctx, id, lease, msg, retryIn, maxAttempts)
	}
	// nack by not acking — message becomes visible again after timeout
	q.mu.Lock()
	delete(q.leases, lease)
	q.mu.Unlock()
	return nil
}

func (q *CFHTTPQueue) Depth(ctx context.Context) (int64, error) {
	if q.useLocal() {
		return q.local.Depth(ctx)
	}
	return 0, nil
}

func (q *CFHTTPQueue) auth(req *http.Request) {
	if q.Token != "" {
		req.Header.Set("Authorization", "Bearer "+q.Token)
	}
}
