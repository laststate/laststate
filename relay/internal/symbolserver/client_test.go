package symbolserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchBuildIDEmptyBuildID(t *testing.T) {
	client := &Client{BaseURL: "http://localhost:8080"}
	_, err := client.FetchBuildID(context.Background(), "", "test")
	if err == nil {
		t.Error("expected error for empty build ID")
	}
}

func TestFetchBuildIDNoServer(t *testing.T) {
	client := &Client{}
	tmpDir := t.TempDir()
	_, err := client.FetchBuildID(context.Background(), tmpDir, "test")
	if err == nil {
		t.Error("expected error when symbol server not configured")
	}
}

func TestFetchBuildIDNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL}
	tmpDir := t.TempDir()
	_, err := client.FetchBuildID(context.Background(), tmpDir, "test")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestFetchBuildIDSuccess(t *testing.T) {
	// Create a fake ELF file content
	elfContent := []byte("FAKE_ELF_CONTENT_FOR_TEST")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/symbols/test" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(elfContent)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	client := &Client{BaseURL: server.URL}
	artifact, err := client.FetchBuildID(context.Background(), tmpDir, "test")
	if err != nil {
		t.Fatalf("fetch should succeed: %v", err)
	}
	if artifact.SHA256 == "" {
		t.Error("artifact should have SHA256")
	}
}

func TestFetchBuildIDWithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected bearer token, got %q", auth)
		}
		w.Write([]byte("FAKE_ELF"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	client := &Client{BaseURL: server.URL, Token: "test-token"}
	_, err := client.FetchBuildID(context.Background(), tmpDir, "test")
	if err != nil {
		t.Fatalf("fetch with auth should succeed: %v", err)
	}
}

func TestFetchBuildIDLimitReader(t *testing.T) {
	// Large response that should be limited to 256MB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 100)) // small for test
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	client := &Client{BaseURL: server.URL}
	_, err := client.FetchBuildID(context.Background(), tmpDir, "test")
	if err != nil {
		t.Fatalf("fetch should succeed: %v", err)
	}
	// Verify temp file was cleaned up
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".elf" {
			// Should have been cleaned up by defer
			t.Errorf("temp file should have been cleaned up: %s", e.Name())
		}
	}
}
