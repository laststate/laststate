//go:build ignore

// End-to-end smoke against a running Trace instance.
// Usage:
//
//	TRACE_URL=http://localhost:8080 TRACE_TOKEN=lst_ingest_... go run ./scripts/smoke_ingest.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/laststate/trace/internal/lep"
)

func main() {
	base := env("TRACE_URL", "http://localhost:8080")
	token := os.Getenv("TRACE_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "TRACE_TOKEN required")
		os.Exit(1)
	}

	// capabilities
	resp, err := http.Get(base + "/v1/relay/capabilities")
	must(err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("capabilities", resp.StatusCode, string(body))

	raw := mustEncodeSample()
	// first ingest
	code, receipt := postIngest(base, token, raw, "evt_smoke_demo_001")
	fmt.Println("ingest1", code, receipt)
	// duplicate
	code2, receipt2 := postIngest(base, token, raw, "evt_smoke_demo_001")
	fmt.Println("ingest2", code2, receipt2)
	if code2 != 409 {
		fmt.Println("WARN: expected 409 on duplicate")
	}
	// wait for worker
	time.Sleep(2 * time.Second)
	resp, err = http.Get(base + "/api/issues")
	must(err)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("issues", resp.StatusCode, string(body))
}

func postIngest(base, token string, raw []byte, eventID string) (int, string) {
	req, err := http.NewRequest(http.MethodPost, base+"/v1/ingest", bytes.NewReader(raw))
	must(err)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", eventID)
	req.Header.Set("X-Last-State-Event-ID", eventID)
	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func mustEncodeSample() []byte {
	idVal := []byte{
		2, 10, 's', 'm', 'o', 'k', 'e', '-', 'd', 'e', 'v', '1',
		3, 5, 'b', 'o', 'a', 'r', 'd',
		7, 5, '0', '.', '1', '.', '0',
		8, 8, 'c', 'a', 'f', 'e', 'b', 'a', 'b', 'e',
	}
	// short CPU: arch, fault, pad, pc, lr
	cpu := make([]byte, 12)
	cpu[0] = 1 // cortex-m
	cpu[1] = 1 // hardfault
	binary.LittleEndian.PutUint32(cpu[4:8], 0x080014a2)
	binary.LittleEndian.PutUint32(cpu[8:12], 0x08001000)
	// fault CFSR div0 bit 25
	fault := make([]byte, 24)
	binary.LittleEndian.PutUint32(fault[0:4], 1<<25)
	payload := lep.EncodeTLVs([]lep.TLV{
		{Type: 1, Value: idVal},
		{Type: 4, Value: cpu},
		{Type: 5, Value: fault},
	})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1, Sequence: 1, EventID: 1001}, payload)
	must(err)
	return raw
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
