package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestEventDispatcherDeliver(t *testing.T) {
	var gotHeader, gotEvent string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-LastState-Signature")
		gotEvent = r.Header.Get("X-LastState-Event")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewEventDispatcher()
	org := uuid.New()
	ep, err := d.RegisterEndpoint(org, srv.URL, "s3cret", []string{"invoice.paid"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_ = ep

	d.Dispatch(context.Background(), BillingEvent{Type: "invoice.paid", OrgID: org, Data: map[string]any{"x": 1}})
	if gotEvent != "invoice.paid" {
		t.Fatalf("event header = %q", gotEvent)
	}
	if len(gotHeader) < 8 || gotHeader[:7] != "sha256=" {
		t.Fatalf("signature header = %q", gotHeader)
	}
	var decoded map[string]any
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, gotBody)
	}

	// Filtered-out type must not deliver.
	gotEvent = ""
	d.Dispatch(context.Background(), BillingEvent{Type: "invoice.failed", OrgID: org})
	if gotEvent != "" {
		t.Fatal("filtered event was delivered")
	}
}

func TestEventDispatcherRetryThenFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewEventDispatcher()
	org := uuid.New()
	ep, _ := d.RegisterEndpoint(org, srv.URL, "s", nil)
	if err := d.emit(context.Background(), ep, BillingEvent{Type: "x", OrgID: org}); err == nil {
		t.Fatal("expected error after retries")
	}
}

func TestRegisterValidation(t *testing.T) {
	d := NewEventDispatcher()
	if _, err := d.RegisterEndpoint(uuid.New(), "ftp://x", "s", nil); err == nil {
		t.Fatal("expected url scheme error")
	}
	if _, err := d.RegisterEndpoint(uuid.New(), "https://x.example/hook", "", nil); err == nil {
		t.Fatal("expected secret error")
	}
	if d.UnregisterEndpoint(uuid.New()) {
		t.Fatal("expected false for unknown endpoint id")
	}
	ep, _ := d.RegisterEndpoint(uuid.New(), "https://x.example/hook", "s", nil)
	if !d.UnregisterEndpoint(ep.ID) {
		t.Fatal("expected unregister true")
	}
}

func TestEmitNilSafe(t *testing.T) {
	var d *EventDispatcher
	d.Emit(context.Background(), uuid.New(), EventInvoicePaid, nil) // must not panic
}
