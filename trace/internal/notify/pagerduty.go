package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/laststate/trace/internal/webhook"
)

// PagerDuty Events API v2 (https://developer.pagerduty.com/docs/ZG9jOjExMDI5NTgw-events-api-v2)
func sendPagerDuty(ctx context.Context, routingKey string, config map[string]any, payload any) Result {
	if routingKey == "" {
		routingKey = str(config, "routing_key", str(config, "integration_key", ""))
	}
	if routingKey == "" {
		return Result{Channel: "pagerduty", Error: "routing_key required", Success: false}
	}
	api := str(config, "api_url", "https://events.pagerduty.com/v2/enqueue")
	if err := webhook.ValidateURL(api); err != nil {
		return Result{Channel: "pagerduty", Error: err.Error()}
	}
	flat := flattenPayload(payload)
	severity := flat["severity"]
	action := "trigger"
	if severity := stringsLower(severity); severity == "info" || severity == "resolved" {
		if severity == "resolved" {
			action = "resolve"
		}
	}
	sev := mapSeverityPD(severity)
	dedup := flat["issue_id"]
	if dedup == "" {
		dedup = flat["title"]
	}
	body, _ := json.Marshal(map[string]any{
		"routing_key":  routingKey,
		"event_action": action,
		"dedup_key":    truncate(dedup, 255),
		"payload": map[string]any{
			"summary":   truncate(flat["title"]+": "+flat["summary"], 1024),
			"severity":  sev,
			"source":    "laststate-trace",
			"component": "firmware",
			"group":     flat["type"],
			"class":     "exception",
			"custom_details": map[string]any{
				"url":      flat["url"],
				"issue_id": flat["issue_id"],
				"raw":      compact(payload),
			},
		},
		"links": func() []map[string]string {
			if flat["url"] == "" {
				return nil
			}
			return []map[string]string{{"href": flat["url"], "text": "Open in Trace"}}
		}(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(body))
	if err != nil {
		return Result{Channel: "pagerduty", Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Channel: "pagerduty", Error: err.Error()}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Result{Channel: "pagerduty", StatusCode: resp.StatusCode, Body: string(b), Success: ok}
}

func mapSeverityPD(s string) string {
	switch stringsLower(s) {
	case "fatal", "critical":
		return "critical"
	case "error":
		return "error"
	case "warning", "warn":
		return "warning"
	case "info", "debug":
		return "info"
	default:
		return "error"
	}
}

func stringsLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Ensure unused import quiet if links always used
var _ = fmt.Sprintf
