//go:build ignore

// Smoke test for zstd-compressed batch ingest (Content-Encoding: zstd),
// mimicking Relay delivery with prefer_zstd enabled.
//
//	TRACE_URL=http://127.0.0.1:18080 TRACE_TOKEN=lst_... go run ./scripts/zstd_smoke.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/klauspost/compress/zstd"
	"github.com/laststate/trace/internal/lep"
)

func main() {
	base := os.Getenv("TRACE_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	token := os.Getenv("TRACE_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "TRACE_TOKEN required")
		os.Exit(1)
	}
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 99, EventID: 4242}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	body, _ := json.Marshal(map[string]any{
		"events": []map[string]any{{"event_id": "evt_zstd_smoke_001", "payload": raw}},
	})
	enc, _ := zstd.NewWriter(nil)
	compressed := enc.EncodeAll(body, nil)
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/events:batch", bytes.NewReader(compressed))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Last-State-Batch-Count", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "request:", err)
		os.Exit(1)
	}
	b, _ := io.ReadAll(resp.Body)
	fmt.Println("zstd batch:", resp.StatusCode, string(b))
	if resp.StatusCode != 200 {
		os.Exit(1)
	}
}
