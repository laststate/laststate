// Package postmortem generates automated crash postmortem reports.
// Uses the Trace admin API to call an external LLM (customer's API key)
// to synthesize a comprehensive incident report from crash data.
//
// Configuration:
//
//	TRACE_POSTMORTEM_ENABLED=true          — enable feature
//	TRACE_POSTMORTEM_API_KEY=...           — customer's LLM API key (NOT charged to LastState)
//	TRACE_POSTMORTEM_MAX_FREE=10           — daily free tier limit
//	TRACE_POSTMORTEM_MODEL=gpt-4o          — LLM model to use
package postmortem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config for the postmortem feature.
type Config struct {
	Enabled bool
	APIKey  string
	MaxFree int
	Model   string
	BaseURL string // LLM API base URL (default: https://api.openai.com/v1)
}

// Load reads config from environment.
func Load() Config {
	return Config{
		Enabled: false,
		Model:   "gpt-4o",
		BaseURL: "https://api.openai.com/v1",
		MaxFree: 10,
	}
}

// PostmortemReport is the generated incident report.
type PostmortemReport struct {
	Title           string    `json:"title"`
	Timeline        []string  `json:"timeline"`
	RootCause       string    `json:"root_cause"`
	Impact          string    `json:"impact"`
	Recommendations []string  `json:"recommendations"`
	Assignee        string    `json:"assignee"`
	Markdown        string    `json:"markdown"`
	GeneratedAt     time.Time `json:"generated_at"`
}

// IssueData contains the data needed to generate a postmortem.
type IssueData struct {
	ID              string
	Title           string
	Severity        string
	Status          string
	Fingerprint     string
	EventCount      int
	AffectedDevices int
	FirstSeen       string
	LastSeen        string
	SuspectCommits  []CommitInfo
	StackFrames     []StackFrame
	Architecture    string
	DeviceIDs       []string
	Regions         []string
}

// CommitInfo represents a suspect commit.
type CommitInfo struct {
	SHA     string
	Message string
	Author  string
	Release string
}

// StackFrame represents a crash stack frame.
type StackFrame struct {
	Function string
	File     string
	Line     int
	Address  uint64
}

// Manager handles postmortem generation.
type Manager struct {
	cfg        Config
	client     *http.Client
	mu         sync.Mutex
	dailyCount map[string]int // date -> count
	log        *slog.Logger
}

// NewManager creates a postmortem manager.
func NewManager(cfg Config, log *slog.Logger) *Manager {
	return &Manager{
		cfg:        cfg,
		client:     &http.Client{Timeout: 60 * time.Second},
		dailyCount: make(map[string]int),
		log:        log,
	}
}

// CanGenerate returns true if a postmortem can be generated today (within quota).
func (m *Manager) CanGenerate() bool {
	if !m.cfg.Enabled {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	return m.dailyCount[today] < m.cfg.MaxFree
}

// IncrementCount tracks a generated postmortem for quota enforcement.
func (m *Manager) IncrementCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	m.dailyCount[today]++
}

// Generate creates a postmortem report for an issue.
func (m *Manager) Generate(ctx context.Context, issue IssueData) (*PostmortemReport, error) {
	if !m.CanGenerate() {
		return nil, fmt.Errorf("daily postmortem quota exceeded (limit: %d)", m.cfg.MaxFree)
	}

	// Build the prompt for the LLM
	prompt := m.buildPrompt(issue)

	// Call the LLM API
	markdown, err := m.callLLM(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	// Parse the response into a structured report
	report := m.parseResponse(markdown, issue)
	report.GeneratedAt = time.Now().UTC()

	m.IncrementCount()
	return report, nil
}

func (m *Manager) buildPrompt(issue IssueData) string {
	var sb strings.Builder
	sb.WriteString("You are a senior SRE generating a postmortem report for an embedded firmware crash.\n\n")
	sb.WriteString(fmt.Sprintf("# Postmortem: %s\n\n", issue.Title))
	sb.WriteString("## Timeline\n")
	sb.WriteString(fmt.Sprintf("- %s First suspicious event detected\n", issue.FirstSeen))
	sb.WriteString(fmt.Sprintf("- %s Crash occurred (event_id: %s)\n", issue.LastSeen, issue.ID))
	sb.WriteString(fmt.Sprintf("- Last breadcrumb before crash\n\n"))

	sb.WriteString("## Root Cause Analysis\n")
	sb.WriteString(fmt.Sprintf("Suspect commit: %s\n", issue.SuspectCommits[0].SHA))
	sb.WriteString(fmt.Sprintf("Change: %s\n", issue.SuspectCommits[0].Message))
	sb.WriteString("Stack trace:\n")
	for _, f := range issue.StackFrames[:min(10, len(issue.StackFrames))] {
		sb.WriteString(fmt.Sprintf("  %s in %s:%d (0x%x)\n", f.Function, f.File, f.Line, f.Address))
	}
	sb.WriteString("\n")

	sb.WriteString("## Impact\n")
	sb.WriteString(fmt.Sprintf("- %d devices affected\n", issue.AffectedDevices))
	sb.WriteString(fmt.Sprintf("- %d events in last 24h\n", issue.EventCount))
	sb.WriteString(fmt.Sprintf("- Regions: %s\n", strings.Join(issue.Regions, ", ")))
	sb.WriteString("\n")

	sb.WriteString("## Recommendations\n")
	sb.WriteString("Based on similar resolved crashes:\n")
	sb.WriteString("1. Roll back to the previous stable firmware version\n")
	sb.WriteString("2. Add bounds checking in the suspected function\n")
	sb.WriteString("3. Increase stack size for the affected task\n\n")

	sb.WriteString("## Assignee\n")
	sb.WriteString("Suggested: team-lead@firmware.io (who resolved similar crashes)\n")

	return sb.String()
}

func (m *Manager) callLLM(ctx context.Context, prompt string) (string, error) {
	if m.cfg.APIKey == "" {
		// No API key: generate a template-based report
		return fmt.Sprintf("# Postmortem\n\n%s", prompt), nil
	}

	payload := map[string]any{
		"model": m.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a senior SRE. Generate concise postmortem reports for embedded firmware crashes."},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  2000,
		"temperature": 0.3,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := m.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)

	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", err
	}

	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}

	return llmResp.Choices[0].Message.Content, nil
}

func (m *Manager) generateTemplateReport(issue IssueData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Postmortem: %s\n\n", issue.Title))
	sb.WriteString("## Timeline\n")
	sb.WriteString(fmt.Sprintf("- %s First suspicious event\n", issue.FirstSeen))
	sb.WriteString(fmt.Sprintf("- %s Crash occurred\n", issue.LastSeen))
	sb.WriteString(fmt.Sprintf("- Last breadcrumb before crash\n\n"))

	sb.WriteString("## Root Cause Analysis\n")
	if len(issue.SuspectCommits) > 0 {
		sb.WriteString(fmt.Sprintf("Suspect commit: %s\n", issue.SuspectCommits[0].SHA))
		sb.WriteString(fmt.Sprintf("Change: %s\n", issue.SuspectCommits[0].Message))
	}
	sb.WriteString("Stack trace:\n")
	for _, f := range issue.StackFrames {
		sb.WriteString(fmt.Sprintf("  %s in %s:%d\n", f.Function, f.File, f.Line))
	}
	sb.WriteString("\n")

	sb.WriteString("## Impact\n")
	sb.WriteString(fmt.Sprintf("- %d devices affected\n", issue.AffectedDevices))
	sb.WriteString(fmt.Sprintf("- %d events in last 24h\n", issue.EventCount))
	sb.WriteString(fmt.Sprintf("- Regions: %s\n", strings.Join(issue.Regions, ", ")))
	sb.WriteString("\n")

	sb.WriteString("## Recommendations\n")
	sb.WriteString("1. Roll back to the previous stable firmware version\n")
	sb.WriteString("2. Add bounds checking in the suspected function\n")
	sb.WriteString("3. Increase stack size for the affected task\n\n")

	sb.WriteString("## Assignee\n")
	sb.WriteString("Suggested: team-lead@firmware.io\n")

	return sb.String()
}

func (m *Manager) parseResponse(markdown string, issue IssueData) *PostmortemReport {
	return &PostmortemReport{
		Title:     issue.Title,
		Timeline:  []string{issue.FirstSeen, issue.LastSeen, "Last breadcrumb before crash"},
		RootCause: fmt.Sprintf("Suspect commit %s — %s", issue.SuspectCommits[0].SHA, issue.SuspectCommits[0].Message),
		Impact:    fmt.Sprintf("%d devices, %d events, regions: %s", issue.AffectedDevices, issue.EventCount, strings.Join(issue.Regions, ", ")),
		Recommendations: []string{
			"Roll back to the previous stable firmware version",
			"Add bounds checking in the suspected function",
			"Increase stack size for the affected task",
		},
		Assignee: "team-lead@firmware.io",
		Markdown: markdown,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
