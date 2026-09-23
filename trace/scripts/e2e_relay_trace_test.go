//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/laststate/trace/internal/lep"
)

// Live Relay↔Trace contract checks against a running Trace instance.
// Required: TRACE_E2E_URL, TRACE_E2E_TOKEN (ingest bearer).
//
// CI starts Trace then runs: go test -tags=e2e ./scripts -count=1 -v

func e2eEnv(t *testing.T) (base, token string) {
	t.Helper()
	base = os.Getenv("TRACE_E2E_URL")
	token = os.Getenv("TRACE_E2E_TOKEN")
	if base == "" || token == "" {
		t.Skip("set TRACE_E2E_URL and TRACE_E2E_TOKEN")
	}
	return base, token
}

func TestCapabilities(t *testing.T) {
	base, _ := e2eEnv(t)
	resp, err := http.Get(base + "/v1/relay/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("capabilities %d %s", resp.StatusCode, body)
	}
	var cap map[string]any
	if err := json.Unmarshal(body, &cap); err != nil {
		t.Fatal(err)
	}
	if cap["api_version"] == nil {
		t.Fatalf("missing api_version: %s", body)
	}
}

func TestRelayIngestDuplicateAndConflict(t *testing.T) {
	base, tok := e2eEnv(t)
	raw1, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 1, EventID: 42}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 2, EventID: 42}, nil)
	if err != nil {
		t.Fatal(err)
	}
	idem := "e2e-relay-" + time.Now().Format("150405.000000")

	// first accept
	code, body := postIngest(t, base, tok, raw1, idem)
	if code != 202 {
		t.Fatalf("ingest %d %s", code, body)
	}

	// true duplicate (same bytes + same Idempotency-Key) → 202 duplicate
	code, body = postIngest(t, base, tok, raw1, idem)
	if code != 202 {
		t.Fatalf("duplicate %d %s", code, body)
	}
	var dup map[string]any
	_ = json.Unmarshal([]byte(body), &dup)
	if dup["duplicate"] != true {
		t.Fatalf("want duplicate body, got %s", body)
	}

	// same event_id, different payload → 422 conflict (must NOT be 202)
	code, body = postIngest(t, base, tok, raw2, idem)
	if code != 422 {
		t.Fatalf("want 422 conflict got %d %s", code, body)
	}
	var errBody map[string]any
	_ = json.Unmarshal([]byte(body), &errBody)
	errObj, _ := errBody["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "conflict" {
		t.Fatalf("want error.code=conflict got %s", body)
	}
}

func TestReady(t *testing.T) {
	base, _ := e2eEnv(t)
	resp, err := http.Get(base + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("ready %d %s", resp.StatusCode, body)
	}
}

func postIngest(t *testing.T, base, token string, raw []byte, eventID string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/v1/ingest", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Idempotency-Key", eventID)
	req.Header.Set("X-Last-State-Event-ID", eventID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, string(body)
}
