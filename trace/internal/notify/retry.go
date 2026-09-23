package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Delivery records one attempt for audit/fan-out UIs.
type Delivery struct {
	Result
	Attempt  int           `json:"attempt"`
	Duration time.Duration `json:"duration_ms"`
	Retried  bool          `json:"retried"`
	Final    bool          `json:"final"`
	Template string        `json:"template,omitempty"`
	Rendered string        `json:"rendered_preview,omitempty"`
}

// Options control resilient fan-out.
type Options struct {
	MaxAttempts int           // default 4
	BaseDelay   time.Duration // default 200ms
	MaxDelay    time.Duration // default 5s
	Template    string        // optional text/html template with {{title}} {{severity}} {{summary}} {{url}}
}

// DeliverWithRetry sends with exponential backoff on 5xx/timeouts.
func DeliverWithRetry(ctx context.Context, kind, target, secret string, config map[string]any, payload any, opts Options) (Result, []Delivery) {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 4
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 200 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 5 * time.Second
	}

	// Apply template rendering into payload envelope
	rendered := RenderTemplate(opts.Template, payload)
	if rendered != "" {
		if m, ok := payload.(map[string]any); ok {
			cp := map[string]any{}
			for k, v := range m {
				cp[k] = v
			}
			cp["rendered"] = rendered
			cp["text"] = rendered
			payload = cp
		}
	}

	var history []Delivery
	var last Result
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		start := time.Now()
		last = Deliver(ctx, kind, target, secret, config, payload)
		d := Delivery{
			Result:   last,
			Attempt:  attempt,
			Duration: time.Since(start),
			Retried:  attempt > 1,
			Final:    attempt == opts.MaxAttempts || last.Success || !retryable(last),
			Template: opts.Template,
		}
		if len(rendered) > 200 {
			d.Rendered = rendered[:200] + "…"
		} else {
			d.Rendered = rendered
		}
		history = append(history, d)
		if last.Success || !retryable(last) {
			break
		}
		if attempt == opts.MaxAttempts {
			break
		}
		delay := backoff(opts.BaseDelay, opts.MaxDelay, attempt)
		select {
		case <-ctx.Done():
			last.Error = ctx.Err().Error()
			last.Success = false
			return last, history
		case <-time.After(delay):
		}
	}
	return last, history
}

func retryable(r Result) bool {
	if r.Success {
		return false
	}
	if r.StatusCode >= 500 || r.StatusCode == 429 || r.StatusCode == 0 {
		return true
	}
	err := strings.ToLower(r.Error)
	return strings.Contains(err, "timeout") ||
		strings.Contains(err, "connection") ||
		strings.Contains(err, "temporary") ||
		strings.Contains(err, "reset")
}

func backoff(base, max time.Duration, attempt int) time.Duration {
	// attempt starts at 1; delay = base * 2^(attempt-1)
	mult := math.Pow(2, float64(attempt-1))
	d := time.Duration(float64(base) * mult)
	if d > max {
		return max
	}
	return d
}

// RenderTemplate fills {{key}} placeholders from payload map.
func RenderTemplate(tmpl string, payload any) string {
	if tmpl == "" {
		tmpl = defaultTemplate()
	}
	m := flattenPayload(payload)
	out := tmpl
	for k, v := range m {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	// strip unresolved tags lightly
	return out
}

func defaultTemplate() string {
	return "[{{severity}}] {{title}}\n{{summary}}\n{{url}}"
}

func flattenPayload(payload any) map[string]string {
	out := map[string]string{
		"title": "Trace alert", "severity": "error", "summary": "", "url": "", "type": "alert",
	}
	m, ok := payload.(map[string]any)
	if !ok {
		out["summary"] = compact(payload)
		return out
	}
	if t, ok := m["type"].(string); ok {
		out["type"] = t
		out["title"] = "Trace: " + t
	}
	if u, ok := m["url"].(string); ok {
		out["url"] = u
	}
	if s, ok := m["summary"].(string); ok {
		out["summary"] = s
	}
	if issue, ok := m["issue"].(map[string]any); ok {
		if t, ok := issue["title"].(string); ok {
			out["title"] = t
		}
		if s, ok := issue["severity"].(string); ok {
			out["severity"] = s
		}
		if s, ok := issue["probable_cause"].(string); ok && out["summary"] == "" {
			out["summary"] = s
		}
		if id, ok := issue["id"]; ok {
			out["issue_id"] = fmt.Sprint(id)
		}
	}
	if out["summary"] == "" {
		b, _ := json.Marshal(m)
		if len(b) > 400 {
			b = b[:400]
		}
		out["summary"] = string(b)
	}
	return out
}
