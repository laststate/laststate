package pr

import (
	"context"
	"log/slog"
	"testing"
)

func TestCreatePRDisabled(t *testing.T) {
	cfg := Config{Enabled: false}
	mgr := NewManager(cfg, slog.Default())

	_, err := mgr.CreatePRForCrash(context.Background(), "issue-1", "Crash", nil, nil)
	if err == nil {
		t.Error("expected error when PR is disabled")
	}
}

func TestCreatePRNoGitHubToken(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		GitHubRepo:  "owner/repo",
		GitHubToken: "",
	}
	mgr := NewManager(cfg, slog.Default())

	_, err := mgr.CreatePRForCrash(context.Background(), "issue-1", "Crash", nil, nil)
	if err == nil {
		t.Error("expected error when GitHub token is not set")
	}
}

func TestBuildPRBody(t *testing.T) {
	cfg := Config{Enabled: true}
	mgr := NewManager(cfg, slog.Default())

	body := mgr.buildPRBody("Test Crash",
		[]SuspectCommit{{SHA: "abc12345", Message: "fix bug", Release: "v1.0"}},
		[]string{"frame1", "frame2"},
		"issue-1")

	if len(body) == 0 {
		t.Error("PR body should not be empty")
	}
}

func TestLoad(t *testing.T) {
	cfg := Load()
	if cfg.Enabled {
		t.Error("PR should be disabled by default")
	}
	if cfg.DefaultBranch != "main" {
		t.Errorf("default branch should be main, got %s", cfg.DefaultBranch)
	}
}
