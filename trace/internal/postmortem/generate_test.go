package postmortem

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoad(t *testing.T) {
	cfg := Load()
	if cfg.Enabled {
		t.Error("postmortem should be disabled by default")
	}
	if cfg.Model != "gpt-4o" {
		t.Errorf("default model should be gpt-4o, got %s", cfg.Model)
	}
	if cfg.MaxFree != 10 {
		t.Errorf("default max free should be 10, got %d", cfg.MaxFree)
	}
}

func TestCanGenerateDisabled(t *testing.T) {
	cfg := Config{Enabled: false}
	mgr := NewManager(cfg, nil)
	if mgr.CanGenerate() {
		t.Error("should not generate when disabled")
	}
}

func TestCanGenerateWithinQuota(t *testing.T) {
	cfg := Config{Enabled: true, MaxFree: 5}
	mgr := NewManager(cfg, nil)
	if !mgr.CanGenerate() {
		t.Error("should generate when enabled and within quota")
	}
}

func TestCanGenerateExceedsQuota(t *testing.T) {
	cfg := Config{Enabled: true, MaxFree: 2}
	mgr := NewManager(cfg, nil)
	mgr.IncrementCount()
	mgr.IncrementCount()
	if mgr.CanGenerate() {
		t.Error("should not generate when quota exceeded")
	}
}

func TestIncrementCount(t *testing.T) {
	cfg := Config{Enabled: true, MaxFree: 2}
	mgr := NewManager(cfg, nil)
	mgr.IncrementCount()
	if !mgr.CanGenerate() {
		t.Error("should still generate after 1 of 2")
	}
	mgr.IncrementCount()
	if mgr.CanGenerate() {
		t.Error("should not generate after 2 of 2")
	}
}

func TestGenerateWithNoAPIKey(t *testing.T) {
	cfg := Config{Enabled: true, APIKey: "", MaxFree: 100}
	mgr := NewManager(cfg, nil)

	issue := IssueData{
		ID:              "issue-1",
		Title:           "Test Crash",
		Severity:        "fatal",
		Status:          "open",
		EventCount:      5,
		AffectedDevices: 3,
		FirstSeen:       "2024-01-01T00:00:00Z",
		LastSeen:        "2024-01-01T00:01:00Z",
		SuspectCommits:  []CommitInfo{{SHA: "abc123", Message: "fix bug"}},
		StackFrames:     []StackFrame{{Function: "main", File: "main.c", Line: 10}},
		Architecture:    "cortex-m",
		Regions:         []string{"us-east-1"},
	}

	report, err := mgr.Generate(context.Background(), issue)
	if err != nil {
		t.Fatalf("generate should succeed without API key: %v", err)
	}
	if report == nil {
		t.Fatal("report should not be nil")
	}
	if report.Title != "Test Crash" {
		t.Errorf("expected title 'Test Crash', got %q", report.Title)
	}
	if len(report.Markdown) == 0 {
		t.Error("markdown should not be empty")
	}
}

func TestGenerateExceedsQuota(t *testing.T) {
	cfg := Config{Enabled: true, MaxFree: 0}
	mgr := NewManager(cfg, nil)

	issue := IssueData{ID: "issue-1", Title: "Test"}
	_, err := mgr.Generate(context.Background(), issue)
	if err == nil {
		t.Error("expected error when quota exceeded")
	}
}

func TestGenerateWithMockLLM(t *testing.T) {
	// Mock LLM API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"# Postmortem\n\nTest report"}}]}`))
	}))
	defer server.Close()

	cfg := Config{
		Enabled: true,
		APIKey:  "test-key",
		MaxFree: 100,
		Model:   "gpt-4o",
		BaseURL: server.URL,
	}
	mgr := NewManager(cfg, nil)

	issue := IssueData{
		ID: "issue-1", Title: "Test",
		FirstSeen: "2024-01-01", LastSeen: "2024-01-01",
		SuspectCommits: []CommitInfo{{SHA: "abc", Message: "fix"}},
		StackFrames:    []StackFrame{{Function: "main"}},
		Regions:        []string{"us"},
	}

	report, err := mgr.Generate(context.Background(), issue)
	if err != nil {
		t.Fatalf("LLM generate should succeed: %v", err)
	}
	if report.Markdown == "" {
		t.Error("report markdown should not be empty")
	}
}

func TestBuildPrompt(t *testing.T) {
	cfg := Config{Enabled: true}
	mgr := NewManager(cfg, nil)

	issue := IssueData{
		ID: "issue-1", Title: "Test",
		FirstSeen: "2024-01-01", LastSeen: "2024-01-01",
		SuspectCommits:  []CommitInfo{{SHA: "abc", Message: "fix"}},
		StackFrames:     []StackFrame{{Function: "main", File: "main.c", Line: 10, Address: 0x1000}},
		EventCount:      5,
		AffectedDevices: 3,
		Regions:         []string{"us-east-1"},
	}

	prompt := mgr.buildPrompt(issue)
	if len(prompt) == 0 {
		t.Error("prompt should not be empty")
	}
}

func TestParseResponse(t *testing.T) {
	cfg := Config{Enabled: true}
	mgr := NewManager(cfg, nil)

	issue := IssueData{
		ID: "issue-1", Title: "Test",
		SuspectCommits:  []CommitInfo{{SHA: "abc", Message: "fix"}},
		EventCount:      5,
		AffectedDevices: 3,
		Regions:         []string{"us"},
	}

	report := mgr.parseResponse("# Report", issue)
	if report.Title != "Test" {
		t.Errorf("expected title Test, got %s", report.Title)
	}
	if len(report.Recommendations) == 0 {
		t.Error("should have recommendations")
	}
}
