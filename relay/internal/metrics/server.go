// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package metrics serves Prometheus text metrics.
package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/laststate/relay/internal/store"
)

type Handler struct{ Store *store.Store }

func (handler Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	status, err := handler.Store.Status(request.Context())
	if err != nil {
		http.Error(writer, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics := []struct {
		name, help string
		value      any
	}{
		{"laststate_relay_events", "Total events persisted by the relay.", status.Events},
		{"laststate_relay_deliveries_pending", "Delivery records ready or waiting for retry.", status.Pending},
		{"laststate_relay_deliveries_active", "Delivery records currently leased by workers.", status.Delivering},
		{"laststate_relay_deliveries_delivered", "Delivery records accepted by destinations.", status.Delivered},
		{"laststate_relay_deliveries_dead_letter", "Delivery records requiring operator action.", status.DeadLetter},
		{"laststate_relay_spool_bytes", "Bytes occupied by persisted event envelopes.", status.SpoolBytes},
		{"laststate_relay_disk_free_bytes", "Free bytes on the relay data filesystem.", status.FreeBytes},
		{"laststate_relay_oldest_pending_seconds", "Age of the oldest pending event.", status.OldestPending.Seconds()},
	}
	for _, metric := range metrics {
		fmt.Fprintf(writer, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", metric.name, metric.help, metric.name, metric.name, metric.value)
	}
	fmt.Fprintf(writer, "# HELP laststate_relay_scrape_timestamp_seconds Unix timestamp of this scrape.\n# TYPE laststate_relay_scrape_timestamp_seconds gauge\nlaststate_relay_scrape_timestamp_seconds %d\n", time.Now().Unix())
}
