// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package delivery

import "testing"

func TestEndpointURLs(t *testing.T) {
	base := "https://trace.example.com/root/"
	ingest, err := ingestURL(base)
	if err != nil || ingest != "https://trace.example.com/root/v1/ingest" {
		t.Fatalf("ingest URL = %q, err=%v", ingest, err)
	}
	batch, err := batchURL(base)
	if err != nil || batch != "https://trace.example.com/root/v1/events:batch" {
		t.Fatalf("batch URL = %q, err=%v", batch, err)
	}
	caps, err := capabilitiesURL(base)
	if err != nil || caps != "https://trace.example.com/root/v1/relay/capabilities" {
		t.Fatalf("capabilities URL = %q, err=%v", caps, err)
	}
}

func TestEndpointURLRejectsCredentials(t *testing.T) {
	if _, err := ingestURL("https://user:pass@example.com"); err == nil {
		t.Fatal("expected URL credentials to be rejected")
	}
	if _, err := batchURL("ftp://example.com"); err == nil {
		t.Fatal("expected unsupported scheme to be rejected")
	}
}
