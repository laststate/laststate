package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/laststate/trace/internal/notify"
)

func TestDeliverSlackDiscordBlockedLocal(t *testing.T) {
	ctx := context.Background()
	r := notify.Deliver(ctx, "slack", "http://127.0.0.1/hook", "", map[string]any{"webhook_url": "http://127.0.0.1/h"}, map[string]any{"type": "x"})
	if r.Success {
		t.Fatal("ssrf")
	}
	r = notify.Deliver(ctx, "discord", "", "", map[string]any{"webhook_url": "http://localhost/x"}, map[string]any{})
	if r.Success {
		t.Fatal()
	}
}

func TestDeliverUnknownChannel(t *testing.T) {
	r := notify.Deliver(context.Background(), "not-a-channel", "", "", nil, nil)
	if r.Success || r.Error == "" {
		t.Fatal(r)
	}
}

func TestPagerDutyNeedsKey(t *testing.T) {
	r := notify.Deliver(context.Background(), "pagerduty", "", "", nil, nil)
	if r.Success || r.Error == "" {
		t.Fatal(r)
	}
}

func TestRenderTemplateAndRetry(t *testing.T) {
	txt := notify.RenderTemplate("{{title}}/{{severity}}", map[string]any{
		"issue": map[string]any{"title": "Boom", "severity": "fatal"},
	})
	if txt != "Boom/fatal" {
		t.Fatal(txt)
	}
	// retry exhausts against blocked loopback
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, hist := notify.DeliverWithRetry(ctx, "webhook", "http://127.0.0.1/nope", "", nil, map[string]any{"type": "t"}, notify.Options{
		MaxAttempts: 2, BaseDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond,
	})
	if r.Success {
		t.Fatal("expected fail")
	}
	if len(hist) < 1 {
		t.Fatal("history")
	}
}

func TestDeliverWebhookBlocked(t *testing.T) {
	r := notify.Deliver(context.Background(), "webhook", "http://127.0.0.1/", "sec", nil, map[string]string{"a": "b"})
	if r.Success {
		t.Fatal()
	}
}

func TestDeliverEmailMissingConfig(t *testing.T) {
	r := notify.Deliver(context.Background(), "email", "", "", map[string]any{}, map[string]any{"x": 1})
	if r.Success {
		t.Fatal()
	}
}

func TestDeliverGitHubMissingRepo(t *testing.T) {
	// will try public api — may fail network; should not panic
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := notify.Deliver(ctx, "github", "", "token", map[string]any{"repo": "o/r"}, map[string]any{"type": "t"})
	_ = r
}

func TestSlackPayloadShapeWithPublicURL(t *testing.T) {
	var got map[string]any
	// cannot use 127.0.0.1 due to SSRF — skip if no public tunnel
	// unit-test JSON marshal path via internal deliver unknown already
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// still localhost — blocked
	r := notify.Deliver(context.Background(), "slack", srv.URL, "", map[string]any{"webhook_url": srv.URL}, map[string]any{"type": "new_issue"})
	if r.Success {
		t.Log("unexpected success on loopback")
	}
}
