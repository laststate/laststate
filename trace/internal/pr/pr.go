// Package pr implements crash-to-PR automation: when a fatal crash occurs,
// a GitHub/GitLab Pull Request is automatically created with:
//   - Symbolicated stack trace
//   - Suspect commits with diffs
//   - Bot comment linking to the Trace issue
//
// The feature is disabled by default and only becomes active when the
// required provider credentials are supplied via environment variables.
//
// Configuration:
//
//	TRACE_CRASH_TO_PR=true              # master switch (optional; auto-enabled when a provider is configured)
//	TRACE_GITHUB_TOKEN=ghp_...          # GitHub personal access token
//	TRACE_GITHUB_REPO=owner/repo        # target repository
//	TRACE_GITHUB_DEFAULT_BRANCH=main    # base branch for PRs (default: main)
//	TRACE_GITLAB_TOKEN=glpat-...        # GitLab token (fallback provider)
//	TRACE_GITLAB_PROJECT_ID=123         # GitLab numeric project id
//	TRACE_GITHUB_API_URL=...            # override GitHub API base (tests/self-hosted)
//	TRACE_GITLAB_API_URL=...            # override GitLab API base (tests/self-hosted)
package pr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultGitHubAPI is the public GitHub REST API base URL.
const DefaultGitHubAPI = "https://api.github.com"

// DefaultGitLabAPI is the public GitLab REST API base URL.
const DefaultGitLabAPI = "https://gitlab.com/api/v4"

// Config for crash-to-PR.
type Config struct {
	GitHubToken     string
	GitLabToken     string
	GitHubRepo      string // "owner/repo"
	GitLabProjectID string
	DefaultBranch   string
	Enabled         bool
	// API base URLs. Empty values fall back to the public endpoints; they are
	// overridable so the flow can be exercised against httptest servers.
	GitHubAPIURL string
	GitLabAPIURL string
}

// Load reads config from environment. Crash-to-PR stays disabled unless a
// provider is configured (GitHub token+repo, or GitLab token+project id), so
// an unconfigured deployment never attempts to reach an external forge.
func Load() Config {
	cfg := Config{
		GitHubToken:     os.Getenv("TRACE_GITHUB_TOKEN"),
		GitLabToken:     os.Getenv("TRACE_GITLAB_TOKEN"),
		GitHubRepo:      os.Getenv("TRACE_GITHUB_REPO"),
		GitLabProjectID: os.Getenv("TRACE_GITLAB_PROJECT_ID"),
		DefaultBranch:   os.Getenv("TRACE_GITHUB_DEFAULT_BRANCH"),
		GitHubAPIURL:    os.Getenv("TRACE_GITHUB_API_URL"),
		GitLabAPIURL:    os.Getenv("TRACE_GITLAB_API_URL"),
	}
	if cfg.DefaultBranch == "" {
		cfg.DefaultBranch = "main"
	}
	githubReady := cfg.GitHubToken != "" && cfg.GitHubRepo != ""
	gitlabReady := cfg.GitLabToken != "" && cfg.GitLabProjectID != ""
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TRACE_CRASH_TO_PR"))) {
	case "false", "0", "off", "no":
		cfg.Enabled = false
	case "true", "1", "on", "yes":
		cfg.Enabled = githubReady || gitlabReady
	default:
		// Unset: auto-enable when a provider is fully configured.
		cfg.Enabled = githubReady || gitlabReady
	}
	return cfg
}

// SuspectCommit represents a commit suspected of causing the crash.
type SuspectCommit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Release string `json:"release"`
	Diff    string `json:"diff,omitempty"`
}

// PRResult holds the result of creating a PR.
type PRResult struct {
	PRURL      string `json:"pr_url"`
	PRNumber   int    `json:"pr_number"`
	BranchName string `json:"branch_name"`
	Error      string `json:"error,omitempty"`
}

// Manager handles PR creation for crashes.
type Manager struct {
	cfg    Config
	client *http.Client
	log    *slog.Logger
}

// NewManager creates a PR manager. Missing API base URLs default to the public
// GitHub/GitLab endpoints.
func NewManager(cfg Config, log *slog.Logger) *Manager {
	if cfg.GitHubAPIURL == "" {
		cfg.GitHubAPIURL = DefaultGitHubAPI
	}
	if cfg.GitLabAPIURL == "" {
		cfg.GitLabAPIURL = DefaultGitLabAPI
	}
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		log:    log,
	}
}

// CreatePRForCrash creates a PR for a given crash issue.
func (m *Manager) CreatePRForCrash(ctx context.Context, issueID string, title string, suspectCommits []SuspectCommit, stackTrace []string) (*PRResult, error) {
	if !m.cfg.Enabled {
		return nil, fmt.Errorf("crash-to-PR is disabled")
	}

	branchName := fmt.Sprintf("fix/crash-%s", issueID)

	// Create the PR body
	body := m.buildPRBody(title, suspectCommits, stackTrace, issueID)

	// Create the PR via GitHub API
	result, err := m.createGitHubPR(ctx, branchName, title, body)
	if err != nil {
		// Fallback to GitLab if GitHub fails
		m.log.Warn("GitHub PR creation failed, trying GitLab", "error", err)
		result, err = m.createGitLabMR(ctx, branchName, title, body)
		if err != nil {
			return nil, fmt.Errorf("failed to create PR/MR: %w", err)
		}
	}

	// Post a comment linking to the Trace issue
	if result.PRURL != "" {
		m.postPRComment(ctx, result, issueID)
	}

	return result, nil
}

func (m *Manager) buildPRBody(title string, commits []SuspectCommit, stackTrace []string, issueID string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Auto-generated PR for crash investigation\n\n"))
	sb.WriteString(fmt.Sprintf("**Trace Issue:** #%s\n\n", issueID))
	sb.WriteString("---\n\n")

	sb.WriteString("### Suspect Commits\n\n")
	for _, c := range commits {
		short := c.SHA
		if len(short) > 8 {
			short = short[:8]
		}
		sb.WriteString(fmt.Sprintf("- `%s` — %s\n", short, c.Message))
		if c.Release != "" {
			sb.WriteString(fmt.Sprintf("  - Release: %s\n", c.Release))
		}
		if c.Diff != "" {
			sb.WriteString("\n<details><summary>diff</summary>\n\n```diff\n")
			sb.WriteString(c.Diff)
			sb.WriteString("\n```\n</details>\n")
		}
	}
	sb.WriteString("\n")

	sb.WriteString("### Stack Trace\n\n")
	sb.WriteString("```\n")
	for i, frame := range stackTrace {
		sb.WriteString(fmt.Sprintf("%d: %s\n", i, frame))
	}
	sb.WriteString("```\n\n")

	sb.WriteString(fmt.Sprintf("---\n*Generated by LastState Trace — [View in Trace](/issues/%s)*\n", issueID))
	return sb.String()
}

func (m *Manager) createGitHubPR(ctx context.Context, branchName, title, body string) (*PRResult, error) {
	if m.cfg.GitHubToken == "" {
		return nil, fmt.Errorf("TRACE_GITHUB_TOKEN not set")
	}

	url := fmt.Sprintf("%s/repos/%s/pulls", strings.TrimRight(m.cfg.GitHubAPIURL, "/"), m.cfg.GitHubRepo)
	payload := map[string]any{
		"title": title,
		"body":  body,
		"head":  branchName,
		"base":  m.cfg.DefaultBranch,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(respBody))
	}

	var pr struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}

	return &PRResult{
		PRURL:      pr.HTMLURL,
		PRNumber:   pr.Number,
		BranchName: branchName,
	}, nil
}

func (m *Manager) createGitLabMR(ctx context.Context, branchName, title, body string) (*PRResult, error) {
	if m.cfg.GitLabToken == "" {
		return nil, fmt.Errorf("TRACE_GITLAB_TOKEN not set")
	}

	url := fmt.Sprintf("%s/projects/%s/merge_requests", strings.TrimRight(m.cfg.GitLabAPIURL, "/"), m.cfg.GitLabProjectID)
	payload := map[string]any{
		"title":         title,
		"description":   body,
		"source_branch": branchName,
		"target_branch": m.cfg.DefaultBranch,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", m.cfg.GitLabToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitLab API %d: %s", resp.StatusCode, string(respBody))
	}

	var mr struct {
		WebURL string `json:"web_url"`
		IID    int    `json:"iid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}

	return &PRResult{
		PRURL:      mr.WebURL,
		PRNumber:   mr.IID,
		BranchName: branchName,
	}, nil
}

func (m *Manager) postPRComment(ctx context.Context, result *PRResult, issueID string) {
	var url string
	isGitLab := strings.Contains(result.PRURL, "gitlab") || m.cfg.GitHubToken == ""
	if !isGitLab {
		url = fmt.Sprintf("%s/repos/%s/issues/%d/comments", strings.TrimRight(m.cfg.GitHubAPIURL, "/"), m.cfg.GitHubRepo, result.PRNumber)
	} else {
		url = fmt.Sprintf("%s/projects/%s/merge_requests/%d/notes", strings.TrimRight(m.cfg.GitLabAPIURL, "/"), m.cfg.GitLabProjectID, result.PRNumber)
	}

	comment := map[string]string{
		"body": fmt.Sprintf("🔗 This crash was likely introduced by a recent commit. [View in LastState Trace](/issues/%s)", issueID),
	}
	jsonBody, _ := json.Marshal(comment)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if isGitLab {
		req.Header.Set("PRIVATE-TOKEN", m.cfg.GitLabToken)
	} else {
		req.Header.Set("Authorization", "token "+m.cfg.GitHubToken)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
	}

	resp, err := m.client.Do(req)
	if err != nil {
		m.log.Warn("failed to post PR comment", "error", err)
		return
	}
	resp.Body.Close()
}
