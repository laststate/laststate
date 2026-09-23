package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSRIMiddlewareServesHTMLWithIntegrity(t *testing.T) {
	dir := t.TempDir()
	html := `<!DOCTYPE html><html><head><script src="/assets/app.js"></script>
<link rel="stylesheet" href="/assets/app.css"></head><body>hi</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}
	// SRI hashes the referenced asset content, so the assets must exist.
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.css"), []byte("body{margin:0}\n"), 0o644); err != nil {
		t.Fatalf("write app.css: %v", err)
	}

	fs := http.Dir(dir)
	h := sriMiddleware(fs)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)

	if !strings.Contains(out, `integrity="sha256-`) {
		t.Fatalf("script tag missing SRI integrity attribute:\n%s", out)
	}
	if !strings.Contains(out, `crossorigin="anonymous"`) {
		t.Fatalf("missing crossorigin attribute:\n%s", out)
	}
	if !strings.Contains(out, `rel="stylesheet"`) {
		t.Fatalf("link stylesheet lost:\n%s", out)
	}
}

func TestSRIMiddlewareServesMissingFileAs404(t *testing.T) {
	dir := t.TempDir()
	fs := http.Dir(dir)
	h := sriMiddleware(fs)

	req := httptest.NewRequest(http.MethodGet, "/missing.txt", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestSRIMiddlewareCachesRepeatedRequests(t *testing.T) {
	dir := t.TempDir()
	html := `<!DOCTYPE html><html><body><script src="/a.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}

	fs := http.Dir(dir)
	h := sriMiddleware(fs)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, rr.Code)
		}
	}
}

func TestSRIMiddlewareOnlyInjectsForHTMLContentType(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(`console.log("x")`), 0o644); err != nil {
		t.Fatalf("write js: %v", err)
	}

	fs := http.Dir(dir)
	h := sriMiddleware(fs)

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body, _ := io.ReadAll(rr.Result().Body)
	if strings.Contains(string(body), "integrity=") {
		t.Fatalf("non-HTML response was modified:\n%s", body)
	}
}
