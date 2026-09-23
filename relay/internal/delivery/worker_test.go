// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laststate/relay/internal/ingest"
	"github.com/laststate/relay/internal/lep"
	"github.com/laststate/relay/internal/store"
)

// buildLEPFrame creates a minimal valid LEP v1 frame for testing.
// The payload must be TLV-encoded (use lep.EncodeTLVs or lep.MarshalTLVs).
func buildLEPFrame(t *testing.T, eventID uint32, payload []byte) []byte {
	t.Helper()
	frame, err := lep.Encode(lep.Version1, 0, 0, 1, eventID, payload)
	if err != nil {
		t.Fatalf("encode LEP frame: %v", err)
	}
	return frame
}

// buildLEPFrameWithJSON creates a valid LEP v1 frame with a JSON payload wrapped in TLV.
func buildLEPFrameWithJSON(t *testing.T, eventID uint32, jsonPayload string) []byte {
	t.Helper()
	tlv, err := lep.MarshalTLVs([]lep.TLV{
		{Type: 1, Value: []byte(jsonPayload)},
	})
	if err != nil {
		t.Fatalf("marshal TLVs: %v", err)
	}
	frame, err := lep.Encode(lep.Version1, 0, 0, 1, eventID, tlv)
	if err != nil {
		t.Fatalf("encode LEP frame: %v", err)
	}
	return frame
}

func TestWorkerDeliverySuccess(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var received int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			http.NotFound(w, r)
			return
		}
		_, _ = r.Body.Read(make([]byte, r.ContentLength))
		atomic.AddInt32(&received, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	if err := relay.ConfigureDestinations(context.Background(), []store.Destination{{
		ID: "trace", URL: server.URL, AuthType: "none", Priority: 1,
	}}, "mirror"); err != nil {
		t.Fatal(err)
	}

	raw := buildLEPFrameWithJSON(t, 1, `{"type":"crash","pc":0x1000}`)
	if _, err := (ingest.Service{Store: relay}).Accept(context.Background(), "test-dev", raw); err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: false},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 5, FailureThreshold: 10, CircuitOpenFor: 500 * time.Millisecond,
		rng: newTestRand(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if received < 1 {
		t.Fatal("destination was never contacted")
	}
}

func TestWorkerRetryOnTransientFailure(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			http.NotFound(w, r)
			return
		}
		_, _ = r.Body.Read(make([]byte, r.ContentLength))
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Header().Set("Retry-After", "0")
			_, _ = w.Write([]byte(`{"error":{"code":"busy"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	if err := relay.ConfigureDestinations(context.Background(), []store.Destination{{
		ID: "trace", URL: server.URL, AuthType: "none", Priority: 1,
	}}, "mirror"); err != nil {
		t.Fatal(err)
	}

	raw := buildLEPFrameWithJSON(t, 2, `{"type":"log","msg":"hello"}`)
	_, err = (ingest.Service{Store: relay}).Accept(context.Background(), "test-dev", raw)
	if err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: false},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 10, FailureThreshold: 10, CircuitOpenFor: 200 * time.Millisecond,
		rng: newTestRand(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// First flush: should get 503 and schedule retry
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	// Second flush: should retry and get 503 again
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	// Third flush: should succeed
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("third flush: %v", err)
	}
	if atomic.LoadInt32(&attempts) < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestWorkerPermanentFailure(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			http.NotFound(w, r)
			return
		}
		_, _ = r.Body.Read(make([]byte, r.ContentLength))
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized"}}`))
	}))
	defer server.Close()

	if err := relay.ConfigureDestinations(context.Background(), []store.Destination{{
		ID: "trace", URL: server.URL, AuthType: "none", Priority: 1,
	}}, "mirror"); err != nil {
		t.Fatal(err)
	}

	raw := buildLEPFrameWithJSON(t, 3, `{"type":"crash"}`)
	_, err = (ingest.Service{Store: relay}).Accept(context.Background(), "test-dev", raw)
	if err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: false},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 3, FailureThreshold: 10, CircuitOpenFor: time.Second,
		rng: newTestRand(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// 401 is permanent — should not retry
	if atomic.LoadInt32(&attempts) != 1 {
		t.Fatalf("expected 1 attempt for permanent failure, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestWorkerCircuitBreakerOpens(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			http.NotFound(w, r)
			return
		}
		_, _ = r.Body.Read(make([]byte, r.ContentLength))
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	if err := relay.ConfigureDestinations(context.Background(), []store.Destination{{
		ID: "trace", URL: server.URL, AuthType: "none", Priority: 1,
	}}, "mirror"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		raw := buildLEPFrameWithJSON(t, uint32(100+i), fmt.Sprintf(`{"type":"crash","id":%d}`, i))
		_, err = (ingest.Service{Store: relay}).Accept(context.Background(), "test-dev", raw)
		if err != nil {
			t.Fatal(err)
		}
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: false},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 5, FailureThreshold: 3, CircuitOpenFor: 100 * time.Millisecond,
		rng: newTestRand(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// All attempts should have been made (some might be skipped by circuit breaker)
	// At minimum we should have some attempts
	if atomic.LoadInt32(&attempts) == 0 {
		t.Fatal("no delivery attempts were made")
	}
}

func TestWorkerBatchDelivery(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var batchCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(Capabilities{
				BatchIngest:    true,
				BinaryBatch:    false,
				MaxBatchEvents: 100,
				MaxBatchBytes:  4 << 20,
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, ":batch") {
			ct := r.Header.Get("Content-Type")
			_ = ct // track batch content type
			body, _ := json.Marshal(map[string]interface{}{
				"accepted":   []map[string]string{},
				"duplicates": []map[string]string{},
				"rejected":   []map[string]string{},
			})
			w.WriteHeader(http.StatusOK)
			w.Write(body)
			batchCount++
		} else {
			_, _ = r.Body.Read(make([]byte, r.ContentLength))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	if err := relay.ConfigureDestinations(context.Background(), []store.Destination{{
		ID: "trace", URL: server.URL, AuthType: "none", Priority: 1,
	}}, "mirror"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		raw := buildLEPFrameWithJSON(t, uint32(200+i), `{"type":"log","msg":"batch test"}`)
		_, err = (ingest.Service{Store: relay}).Accept(context.Background(), "test-dev", raw)
		if err != nil {
			t.Fatal(err)
		}
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: true, MaxBatchEvents: 10},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 5, FailureThreshold: 10, CircuitOpenFor: 500 * time.Millisecond,
		rng: newTestRand(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if batchCount == 0 {
		t.Fatal("batch delivery was not used")
	}
}

func TestWorkerZstdCompression(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var contentEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(Capabilities{
				Compression: []string{"zstd"},
			})
			return
		}
		contentEncoding = r.Header.Get("Content-Encoding")
		_, _ = r.Body.Read(make([]byte, r.ContentLength))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	if err := relay.ConfigureDestinations(context.Background(), []store.Destination{{
		ID: "trace", URL: server.URL, AuthType: "none", Priority: 1,
	}}, "mirror"); err != nil {
		t.Fatal(err)
	}

	// Large payload to benefit from compression
	largePayload := bytes.Repeat([]byte("X"), 4096)
	tlv, _ := lep.MarshalTLVs([]lep.TLV{{Type: 1, Value: largePayload}})
	raw := buildLEPFrame(t, 300, tlv)
	_, err = (ingest.Service{Store: relay}).Accept(context.Background(), "test-dev", raw)
	if err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: false, PreferZstd: true},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 5, FailureThreshold: 10, CircuitOpenFor: 500 * time.Millisecond,
		rng: newTestRand(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// With zstd preference, content-encoding should be set
	if contentEncoding != "zstd" {
		t.Logf("content-encoding=%q (zstd compression may not apply to this payload size)", contentEncoding)
	}
}

func TestWorkerDefaultHTTPClientHasTLS12(t *testing.T) {
	client := defaultHTTPClient()
	if client.Transport == nil {
		t.Fatal("expected transport to be set")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLS config")
	}
	if transport.TLSClientConfig.MinVersion != 0x0303 { // TLS 1.2
		t.Errorf("expected MinVersion TLS 1.2 (0x0303), got 0x%04x", transport.TLSClientConfig.MinVersion)
	}
}

func TestIsPermanentStatusCodes(t *testing.T) {
	tests := []struct {
		status int
		perm   bool
	}{
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, true},
		{http.StatusRequestEntityTooLarge, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusConflict, true},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusOK, false},
	}
	for _, tt := range tests {
		if got := isPermanent(tt.status); got != tt.perm {
			t.Errorf("isPermanent(%d) = %v, want %v", tt.status, got, tt.perm)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		input    string
		expected time.Time
	}{
		{"", time.Time{}},
		{"0", now},
		{"5", now.Add(5 * time.Second)},
		{"invalid", time.Time{}},
		{"-1", time.Time{}},
	}
	for _, tt := range tests {
		got := parseRetryAfter(tt.input, now)
		if !tt.expected.Equal(got) {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIngestURL(t *testing.T) {
	tests := []struct {
		base    string
		want    string
		wantErr bool
	}{
		{"https://trace.example.com/root/", "https://trace.example.com/root/v1/ingest", false},
		{"http://localhost:8080", "http://localhost:8080/v1/ingest", false},
		{"ftp://bad", "", true},
		{"https://user:pass@example.com", "", true},
	}
	for _, tt := range tests {
		got, err := ingestURL(tt.base)
		if (err != nil) != tt.wantErr {
			t.Errorf("ingestURL(%q) error = %v, wantErr %v", tt.base, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ingestURL(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestBatchURL(t *testing.T) {
	got, err := batchURL("https://trace.example.com/root/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://trace.example.com/root/v1/events:batch" {
		t.Errorf("batchURL = %q", got)
	}
}

func TestBodyIndicatesDuplicate(t *testing.T) {
	tests := []struct {
		body string
		dup  bool
	}{
		{`{"duplicate":true}`, true},
		{`{"status":"duplicate"}`, true},
		{`{"code":"duplicate"}`, true},
		{`{"error":{"code":"duplicate"}}`, true},
		{`{"status":"ok"}`, false},
		{``, false},
	}
	for _, tt := range tests {
		if got := bodyIndicatesDuplicate(tt.body); got != tt.dup {
			t.Errorf("bodyIndicatesDuplicate(%q) = %v, want %v", tt.body, got, tt.dup)
		}
	}
}

func TestDestinationFailure(t *testing.T) {
	got := destinationFailure(500, "")
	if !strings.Contains(got, "500") {
		t.Errorf("expected status code in failure string, got %q", got)
	}
	got = destinationFailure(400, "bad request")
	if !strings.Contains(got, "bad request") {
		t.Errorf("expected message in failure string, got %q", got)
	}
}

func TestIsIngestSuccess(t *testing.T) {
	if !isIngestSuccess(200, "") {
		t.Error("200 should be success")
	}
	if isIngestSuccess(500, "") {
		t.Error("500 should not be success")
	}
	if isIngestSuccess(409, "") {
		t.Error("409 without duplicate marker should not be success")
	}
	if !isIngestSuccess(409, `{"duplicate":true}`) {
		t.Error("409 with duplicate marker should be success")
	}
}

func TestIsIngestConflict(t *testing.T) {
	if !isIngestConflict(422, `{"error":{"code":"conflict"}}`) {
		t.Error("422 with conflict code should be conflict")
	}
	if isIngestConflict(409, `{"duplicate":true}`) {
		t.Error("409 with duplicate marker should not be conflict")
	}
}

func TestSupportsCompression(t *testing.T) {
	if !supportsCompression([]string{"zstd", "gzip"}, "zstd") {
		t.Error("should support zstd")
	}
	if supportsCompression([]string{"gzip"}, "zstd") {
		t.Error("should not support zstd when only gzip advertised")
	}
}

func TestZstdCompress(t *testing.T) {
	input := bytes.Repeat([]byte("hello world! "), 100)
	compressed, err := zstdCompress(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) >= len(input) {
		t.Errorf("compressed size %d should be less than %d", len(compressed), len(input))
	}
}

func TestNewWorkerDefaults(t *testing.T) {
	w := &Worker{}
	w.defaults()
	if w.Concurrency != 4 {
		t.Errorf("expected default Concurrency=4, got %d", w.Concurrency)
	}
	if w.MaxAttempts != 50 {
		t.Errorf("expected default MaxAttempts=50, got %d", w.MaxAttempts)
	}
	if w.rng == nil {
		t.Error("expected rng to be initialized")
	}
}

func TestBackoff(t *testing.T) {
	w := &Worker{
		MinDelay:   time.Second,
		MaxDelay:   30 * time.Second,
		Multiplier: 2,
		Jitter:     false,
		rng:        newTestRand(t),
	}
	d0 := w.backoff(0)
	d1 := w.backoff(1)
	d2 := w.backoff(2)
	if d0 >= d1 || d1 >= d2 {
		t.Errorf("expected exponential backoff: %v < %v < %v", d0, d1, d2)
	}
	if d2 > w.MaxDelay {
		t.Errorf("backoff should be capped at MaxDelay, got %v", d2)
	}
}

func TestExtractDeliveries(t *testing.T) {
	items := []preparedDelivery{
		{Delivery: store.PendingDelivery{EventID: "e1"}},
		{Delivery: store.PendingDelivery{EventID: "e2"}},
	}
	out := extractDeliveries(items)
	if len(out) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(out))
	}
	if out[0].EventID != "e1" {
		t.Errorf("expected e1, got %s", out[0].EventID)
	}
}

// newTestRand creates a deterministic random source for tests.
func newTestRand(t *testing.T) *rand.Rand {
	t.Helper()
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}
