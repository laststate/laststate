// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/laststate/relay/internal/config"
	"github.com/laststate/relay/internal/ui"
)

// infoOptions configures the `info` command (and the no-args default).
type infoOptions struct {
	configPath string
	dataDir    string
	adminURL   string // optional override; otherwise picked from config
}

// infoCmd renders a self-diagnosis page: version, runtime, configuration,
// project layout, server connectivity, and a quick command map. The page
// is the default when the binary is invoked without arguments so that
// "what does this thing do?" is a one-keystroke answer.
func infoCmd(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	opts := infoOptions{
		configPath: "relay.yaml",
		dataDir:    "data",
	}
	fs.StringVar(&opts.configPath, "config", opts.configPath, "Configuration file (used to discover sources/destinations)")
	fs.StringVar(&opts.dataDir, "data-dir", opts.dataDir, "Data directory (used to read spool counters)")
	fs.StringVar(&opts.adminURL, "admin", "", "Admin listener URL (overrides the one in the config)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, ui.Section("About"))
	fmt.Fprintln(os.Stdout, ui.KVTable(buildAboutTable()))
	fmt.Fprintln(os.Stdout)

	cfg, cfgFound := tryLoadConfig(opts.configPath)
	fmt.Fprintln(os.Stdout, ui.Section("Project"))
	fmt.Fprintln(os.Stdout, ui.KVTable(buildProjectTable(opts, cfg, cfgFound)))
	fmt.Fprintln(os.Stdout)

	fmt.Fprintln(os.Stdout, ui.Section("Health"))
	for _, line := range buildHealthTable(opts) {
		fmt.Fprintln(os.Stdout, "  "+line)
	}
	fmt.Fprintln(os.Stdout)

	// Server reachability checks — keep them snappy with a tight timeout so
	// the page is always responsive even on offline machines.
	fmt.Fprintln(os.Stdout, ui.Section("Servers"))
	for _, line := range buildServerTable(cfg, cfgFound, opts) {
		fmt.Fprintln(os.Stdout, "  "+line)
	}
	fmt.Fprintln(os.Stdout)

	// If the admin listener is reachable, fetch the live snapshot and
	// print it as a JSON block — easy to grep and to spot-check.
	if adminURL := resolveAdminURL(cfg, cfgFound, opts); adminURL != "" {
		if snap, ok := fetchAdminStatus(adminURL); ok {
			fmt.Fprintln(os.Stdout, ui.Section("Admin /v1/status (live)"))
			pretty, _ := json.MarshalIndent(snap, "  ", "  ")
			fmt.Fprintf(os.Stdout, "  %s\n", ui.Styles().TableCell.Render(string(pretty)))
			fmt.Fprintln(os.Stdout)
		}
	}

	fmt.Fprintln(os.Stdout, ui.Section("Quick start"))
	for _, line := range buildQuickStart(cfg, cfgFound, opts) {
		fmt.Fprintln(os.Stdout, "  "+line)
	}
	fmt.Fprintln(os.Stdout)

	fmt.Fprintln(os.Stdout, ui.Section("Available commands"))
	commands := [][2]string{
		{"run", "start the relay daemon with the live TUI dashboard"},
		{"collect", "read envelopes from a serial port (live TUI)"},
		{"inspect <file>", "print a single LEP envelope"},
		{"analyze <file>", "run a deeper analysis on a LEP envelope"},
		{"import <file>", "import a .lep file into the spool"},
		{"export <id>", "export a stored event back to a .lep file"},
		{"replay", "replay one or all events to a destination"},
		{"status", "show spool and delivery counters"},
		{"doctor", "run a quick health check"},
		{"artifacts add/list", "manage the local artifact catalog"},
		{"config validate <file>", "validate a relay.yaml configuration"},
		{"version", "print the CLI version"},
		{"info", "show this self-diagnosis page (default)"},
	}
	t := ui.NewTable(ui.Column{Title: "command", Width: 24}, ui.Column{Title: "description"})
	for _, c := range commands {
		t.AppendRow(c[0], c[1])
	}
	fmt.Fprintln(os.Stdout, t.String())
	fmt.Fprintln(os.Stdout)

	fmt.Fprintln(os.Stdout, ui.Styles().Muted.Render("  Tip: use --no-color to disable ANSI styling, --color to force-enable it."))
	return nil
}

// buildAboutTable assembles the static "about" card.
func buildAboutTable() [][2]string {
	now := time.Now().Format("2006-01-02")
	return [][2]string{
		{"cli version", cliVersion},
		{"built", now},
		{"go runtime", runtime.Version()},
		{"goos / goarch", runtime.GOOS + " / " + runtime.GOARCH},
		{"project", "Last State Relay"},
		{"tagline", "offline-first LEP gateway"},
	}
}

// tryLoadConfig attempts to read the config file without surfacing an
// error. A missing file is not a failure for the info command — the user
// may simply not have configured a relay yet.
func tryLoadConfig(path string) (config.Config, bool) {
	if path == "" {
		return config.Config{}, false
	}
	if _, err := os.Stat(path); err != nil {
		return config.Config{}, false
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, false
	}
	return cfg, true
}

// buildProjectTable returns the KV pairs for the "Project" section.
func buildProjectTable(opts infoOptions, cfg config.Config, cfgFound bool) [][2]string {
	pairs := [][2]string{
		{"working dir", cwdOrUnknown()},
		{"data dir", opts.dataDir + presentBadge(fileExists(opts.dataDir))},
		{"config file", opts.configPath + presentBadge(cfgFound)},
		{"instance", instanceName(cfg, cfgFound)},
		{"sources", fmt.Sprintf("%d", len(cfg.Sources))},
		{"destinations", fmt.Sprintf("%d", len(cfg.Destinations))},
	}
	return pairs
}

// buildHealthTable probes a few runtime invariants. Each line is already
// styled so the caller can print it directly.
func buildHealthTable(opts infoOptions) []string {
	lines := []string{}
	lines = append(lines, healthLine("data directory "+opts.dataDir, fileExists(opts.dataDir), "create it with `laststate-relay run` or pass --data-dir"))

	if fileExists(opts.dataDir) {
		dbPath := filepath.Join(opts.dataDir, "relay.db")
		lines = append(lines, healthLine("sqlite database", fileExists(dbPath), dbPath))
	}

	if fileExists(opts.configPath) {
		lines = append(lines, healthLine("config parseable", true, opts.configPath))
	} else {
		lines = append(lines, healthLine("config parseable", false, "missing — copy relay.yaml.example to "+opts.configPath))
	}

	lines = append(lines, healthLine("go modules cached", fileExists("go.mod"), ""))
	lines = append(lines, healthLine("git working tree", fileExists(".git"), ""))
	return lines
}

// buildServerTable pings each known endpoint with a tight timeout. The
// results are returned as styled strings ready to print.
func buildServerTable(cfg config.Config, cfgFound bool, opts infoOptions) []string {
	lines := []string{}
	checks := collectChecks(cfg, cfgFound, opts)

	if len(checks) == 0 {
		lines = append(lines, "  "+ui.Styles().Muted.Render("No sources, destinations, or admin listener configured."))
		return lines
	}

	type result struct {
		name string
		line string
	}
	results := make([]result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		i, c := i, c
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = result{name: c.name, line: renderCheck(c)}
		}()
	}
	wg.Wait()

	// Stable order — same as the input slice, not the goroutine order.
	for _, r := range results {
		lines = append(lines, r.line)
	}
	return lines
}

type check struct {
	kind   string // "tcp", "http", "serial"
	name   string
	addr   string
	url    string
	detail string
}

func collectChecks(cfg config.Config, cfgFound bool, opts infoOptions) []check {
	var checks []check
	if !cfgFound {
		return checks
	}
	for _, src := range cfg.Sources {
		if !src.IsEnabled() {
			continue
		}
		switch src.Type {
		case "tcp":
			checks = append(checks, check{kind: "tcp", name: "source " + src.ID, addr: src.TCP.Listen, detail: "TCP listener"})
		case "http":
			checks = append(checks, check{kind: "tcp", name: "source " + src.ID, addr: src.HTTP.Listen, detail: "HTTP listener"})
		case "udp":
			checks = append(checks, check{kind: "udp", name: "source " + src.ID, addr: src.UDP.Listen, detail: "UDP listener"})
		case "serial":
			checks = append(checks, check{kind: "serial", name: "source " + src.ID, detail: src.Serial.Device})
		case "directory":
			checks = append(checks, check{kind: "dir", name: "source " + src.ID, detail: src.DirectoryPath()})
		case "mqtt":
			checks = append(checks, check{kind: "http", name: "source " + src.ID, detail: "mqtt " + src.MQTT.Broker})
		case "adapter":
			checks = append(checks, check{kind: "dir", name: "source " + src.ID, detail: "adapter " + src.Adapter.Command})
		case "ble":
			checks = append(checks, check{kind: "dir", name: "source " + src.ID, detail: "ble " + src.BLE.Adapter})
		case "can":
			checks = append(checks, check{kind: "dir", name: "source " + src.ID, detail: "can " + src.CAN.Interface})
		case "lorawan":
			checks = append(checks, check{kind: "http", name: "source " + src.ID, detail: "lorawan " + src.LoRaWAN.Server})
		}
	}
	for _, dst := range cfg.Destinations {
		if !dst.IsEnabled() {
			continue
		}
		checks = append(checks, check{kind: "http", name: "destination " + dst.ID, url: dst.URL, detail: "POST " + dst.URL + "/v1/ingest"})
	}
	if cfg.Admin.Listen != "" {
		checks = append(checks, check{kind: "http", name: "admin listener", addr: cfg.Admin.Listen, url: "http://" + stripScheme(cfg.Admin.Listen) + "/v1/status", detail: "admin API"})
	}
	return checks
}

func renderCheck(c check) string {
	switch c.kind {
	case "tcp":
		ok, latency := tcpProbe(c.addr)
		return probeLine(c.name+" @ "+c.addr, ok, latency, c.detail)
	case "http":
		ok, latency := httpProbe(c.url)
		return probeLine(c.name, ok, latency, c.detail)
	case "serial":
		ok, latency := serialProbe(c.detail)
		return probeLine(c.name, ok, latency, "serial device")
	case "udp":
		ok, latency := udpProbe(c.addr)
		return probeLine(c.name+" @ "+c.addr, ok, latency, c.detail)
	case "dir":
		ok := fileExists(c.detail)
		return probeLine(c.name, ok, 0, c.detail)
	}
	return probeLine(c.name, false, 0, c.detail)
}

func probeLine(name string, ok bool, latency time.Duration, detail string) string {
	s := ui.Styles()
	badge := "✗"
	stateStyle := s.Err
	if ok {
		badge = "✓"
		stateStyle = s.OK
	}
	extra := ""
	if latency > 0 {
		extra = " " + s.Muted.Render(fmt.Sprintf("(%s)", latency.Round(time.Millisecond)))
	}
	detailStr := ""
	if detail != "" {
		detailStr = "  " + s.Muted.Render(detail)
	}
	return fmt.Sprintf("%s %s %s%s%s", stateStyle.Render(badge), s.Bold.Render(name), detailStr, extra, "")
}

// tcpProbe attempts to open a TCP connection. It always returns quickly
// thanks to the explicit deadline; the boolean reports reachability.
func tcpProbe(addr string) (bool, time.Duration) {
	if addr == "" {
		return false, 0
	}
	start := time.Now()
	d := net.Dialer{Timeout: 250 * time.Millisecond}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return false, time.Since(start)
	}
	_ = conn.Close()
	return true, time.Since(start)
}

func udpProbe(addr string) (bool, time.Duration) {
	if addr == "" {
		return false, 0
	}
	start := time.Now()
	conn, err := net.DialTimeout("udp", addr, 250*time.Millisecond)
	if err != nil {
		return false, time.Since(start)
	}
	_ = conn.Close()
	return true, time.Since(start)
}

// httpProbe performs a quick GET against the URL and reports the result.
func httpProbe(url string) (bool, time.Duration) {
	if url == "" {
		return false, 0
	}
	client := &http.Client{Timeout: 400 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, 0
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, time.Since(start)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false, time.Since(start)
	}
	return true, time.Since(start)
}

// serialProbe is a best-effort stat on a serial device. We avoid actually
// opening the port because that can hold exclusive locks on Windows; the
// stat is enough to tell the user whether the device is plugged in.
func serialProbe(path string) (bool, time.Duration) {
	if path == "" {
		return false, 0
	}
	start := time.Now()
	_, err := os.Stat(path)
	return err == nil, time.Since(start)
}

// resolveAdminURL returns the admin URL to query for a live snapshot, or
// empty if no admin listener is configured.
func resolveAdminURL(cfg config.Config, cfgFound bool, opts infoOptions) string {
	if opts.adminURL != "" {
		return opts.adminURL
	}
	if !cfgFound || cfg.Admin.Listen == "" {
		return ""
	}
	return "http://" + stripScheme(cfg.Admin.Listen) + "/v1/status"
}

func fetchAdminStatus(url string) (map[string]any, bool) {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, false
	}
	return payload, true
}

func buildQuickStart(cfg config.Config, cfgFound bool, opts infoOptions) []string {
	s := ui.Styles()
	if !cfgFound {
		return []string{
			s.Muted.Render("No config found. Try one of:") + "\n" +
				"    " + s.Bold.Render("cp relay.yaml.example relay.yaml") + s.Muted.Render("      # copy the example config") + "\n" +
				"    " + s.Bold.Render("go run ./cmd/laststate-relay run --config relay.yaml") + s.Muted.Render("    # start the relay"),
		}
	}
	return []string{
		s.Muted.Render("Run the relay with:") + "\n" +
			"    " + s.Bold.Render("laststate-relay run --config "+opts.configPath),
	}
}

// presentBadge returns a green check or red cross rendered with the
// package styles. The function exists so the project table can flip a
// single character instead of a full line.
func presentBadge(ok bool) string {
	if !ui.IsTTY() {
		if ok {
			return " [ok]"
		}
		return " [missing]"
	}
	if ok {
		return " " + ui.Styles().OK.Render("✓")
	}
	return " " + ui.Styles().Err.Render("✗")
}

// healthLine returns a styled "label status detail" triple. The icon
// comes from the ui package so health and probe lines share the same
// visual language.
func healthLine(label string, ok bool, detail string) string {
	s := ui.Styles()
	icon := ui.Icon("ok")
	style := s.OK
	if !ok {
		icon = ui.Icon("warn")
		style = s.Warn
	}
	out := fmt.Sprintf("%s %s %s", style.Render(icon), s.Bold.Render(label), s.OK.Render(presentWord(ok)))
	if detail != "" {
		out += "  " + s.Muted.Render(detail)
	}
	return out
}

func presentWord(ok bool) string {
	if ok {
		return "ready"
	}
	return "not ready"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func cwdOrUnknown() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "(unknown)"
	}
	return cwd
}

func instanceName(cfg config.Config, found bool) string {
	if !found || cfg.Instance.Name == "" {
		return ui.Styles().Muted.Render("(not configured)")
	}
	return cfg.Instance.Name
}

// stripScheme removes an optional http:// or https:// prefix so the same
// listen address can be used in TCP probes and admin URL construction.
func stripScheme(s string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return s
}
