// Package notify delivers multi-channel alerts (webhook, slack, discord, email stub, github stub).
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/laststate/trace/internal/webhook"
)

type Result struct {
	Channel    string
	StatusCode int
	Body       string
	Success    bool
	Error      string
}

func Deliver(ctx context.Context, kind, targetOrURL, secret string, config map[string]any, payload any) Result {
	switch strings.ToLower(kind) {
	case "webhook", "":
		return sendWebhook(ctx, targetOrURL, secret, payload)
	case "slack":
		url := str(config, "webhook_url", targetOrURL)
		return sendSlack(ctx, url, payload)
	case "discord":
		url := str(config, "webhook_url", targetOrURL)
		return sendDiscord(ctx, url, payload)
	case "email":
		return sendEmail(ctx, config, payload)
	case "github", "gitlab":
		return sendGitIssue(ctx, kind, config, secret, payload)
	case "pagerduty", "pd":
		return sendPagerDuty(ctx, targetOrURL, config, payload)
	default:
		return Result{Channel: kind, Error: "unknown channel", Success: false}
	}
}

func sendWebhook(ctx context.Context, url, secret string, payload any) Result {
	d, err := webhook.Send(ctx, url, secret, payload)
	if err != nil {
		return Result{Channel: "webhook", Error: err.Error()}
	}
	return Result{Channel: "webhook", StatusCode: d.StatusCode, Body: d.Body, Success: d.Success}
}

func sendSlack(ctx context.Context, url string, payload any) Result {
	if err := webhook.ValidateURL(url); err != nil {
		return Result{Channel: "slack", Error: err.Error()}
	}
	text := RenderTemplate("", payload)
	if m, ok := payload.(map[string]any); ok {
		if t, ok := m["text"].(string); ok && t != "" {
			text = t
		} else if t, ok := m["rendered"].(string); ok && t != "" {
			text = t
		}
	}
	// Block kit-ish attachment for richer mobile push
	flat := flattenPayload(payload)
	body, _ := json.Marshal(map[string]any{
		"text": text,
		"blocks": []map[string]any{
			{"type": "header", "text": map[string]string{"type": "plain_text", "text": truncate(flat["title"], 140)}},
			{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": truncate(flat["summary"], 2000)}},
			{"type": "context", "elements": []map[string]string{
				{"type": "mrkdwn", "text": "*severity* " + flat["severity"]},
			}},
		},
	})
	return postJSON(ctx, "slack", url, body)
}

func sendDiscord(ctx context.Context, url string, payload any) Result {
	if err := webhook.ValidateURL(url); err != nil {
		return Result{Channel: "discord", Error: err.Error()}
	}
	content := RenderTemplate("", payload)
	if m, ok := payload.(map[string]any); ok {
		if t, ok := m["rendered"].(string); ok && t != "" {
			content = t
		}
	}
	if len(content) > 1900 {
		content = content[:1900]
	}
	flat := flattenPayload(payload)
	body, _ := json.Marshal(map[string]any{
		"content": content,
		"embeds": []map[string]any{{
			"title":       truncate(flat["title"], 200),
			"description": truncate(flat["summary"], 1500),
			"color":       0xE74C3C,
			"url":         flat["url"],
		}},
	})
	return postJSON(ctx, "discord", url, body)
}

func sendEmail(ctx context.Context, config map[string]any, payload any) Result {
	host := str(config, "smtp_host", "")
	from := str(config, "from", "")
	to := str(config, "to", "")
	user := str(config, "username", "")
	pass := str(config, "password", "")
	if host == "" || from == "" || to == "" {
		return Result{Channel: "email", Error: "smtp_host, from, to required", Success: false}
	}
	flat := flattenPayload(payload)
	subj := str(config, "subject", "[Trace] "+flat["title"])
	body := RenderTemplate(str(config, "template", "Severity: {{severity}}\n\n{{title}}\n\n{{summary}}\n\n{{url}}"), payload)
	msg := []byte("To: " + to + "\r\nSubject: " + subj + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body)
	addr := host
	if !strings.Contains(addr, ":") {
		addr += ":587"
	}
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, strings.Split(host, ":")[0])
	}
	// best-effort; context cancel not supported by net/smtp
	errCh := make(chan error, 1)
	go func() { errCh <- smtp.SendMail(addr, auth, from, []string{to}, msg) }()
	select {
	case <-ctx.Done():
		return Result{Channel: "email", Error: ctx.Err().Error()}
	case err := <-errCh:
		if err != nil {
			return Result{Channel: "email", Error: err.Error()}
		}
		return Result{Channel: "email", Success: true, StatusCode: 250}
	}
}

func sendGitIssue(ctx context.Context, kind string, config map[string]any, token string, payload any) Result {
	api := str(config, "api_url", "")
	repo := str(config, "repo", "")
	if api == "" {
		if kind == "gitlab" {
			api = "https://gitlab.com/api/v4/projects/" + strings.ReplaceAll(repo, "/", "%2F") + "/issues"
		} else {
			api = "https://api.github.com/repos/" + repo + "/issues"
		}
	}
	if err := webhook.ValidateURL(api); err != nil {
		return Result{Channel: kind, Error: err.Error()}
	}
	title := "Trace alert"
	body := compact(payload)
	if m, ok := payload.(map[string]any); ok {
		if t, ok := m["type"].(string); ok {
			title = "Trace: " + t
		}
	}
	var reqBody []byte
	if kind == "gitlab" {
		reqBody, _ = json.Marshal(map[string]string{"title": title, "description": body})
	} else {
		reqBody, _ = json.Marshal(map[string]string{"title": title, "body": body})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(reqBody))
	if err != nil {
		return Result{Channel: kind, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if kind == "gitlab" {
		req.Header.Set("PRIVATE-TOKEN", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{Channel: kind, Error: err.Error()}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Result{Channel: kind, StatusCode: resp.StatusCode, Body: string(b), Success: ok}
}

func postJSON(ctx context.Context, channel, url string, body []byte) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{Channel: channel, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Channel: channel, Error: err.Error()}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Result{Channel: channel, StatusCode: resp.StatusCode, Body: string(b), Success: ok}
}

func str(m map[string]any, k, def string) string {
	if m == nil {
		return def
	}
	if v, ok := m[k].(string); ok && v != "" {
		return v
	}
	return def
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
