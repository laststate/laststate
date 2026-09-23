// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package delivery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/laststate/relay/internal/ingest"
	"github.com/laststate/relay/internal/lep"
	"github.com/laststate/relay/internal/store"
)

func TestRetryAfter503ThenSuccess(t *testing.T) {
	dir := t.TempDir()
	relay, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "mirror",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/relay/capabilities" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.ReadAll(r.Body)
		n := hits.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "busy", http.StatusServiceUnavailable)
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
	raw, err := lep.Encode(lep.Version1, 1, 0, 1, 42, []byte{1, 0, 1, 0, 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ingest.Service{Store: relay}).Accept(context.Background(), "s", raw); err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		Store: relay,
		Destinations: map[string]RuntimeDestination{
			"trace": {Client: server.Client(), BatchEnabled: false},
		},
		Concurrency: 1, MinDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond,
		Multiplier: 1.5, MaxAttempts: 5, FailureThreshold: 10, CircuitOpenFor: time.Second,
	}
	_ = worker.Flush(context.Background())
	time.Sleep(20 * time.Millisecond)
	if err := worker.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() < 1 {
		t.Fatal("destination was never contacted")
	}
}
