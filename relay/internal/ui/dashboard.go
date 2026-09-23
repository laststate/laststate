// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EventKind categorises a dashboard event for the log panel.
type EventKind int

const (
	EventInfo EventKind = iota
	EventStored
	EventDuplicate
	EventDelivered
	EventFailed
	EventDead
	EventSourceUp
	EventSourceDown
	EventDestinationUp
	EventDestinationDown
	EventError
)

// Event is a single log entry shown in the dashboard's event panel. The
// timestamp is supplied by the publisher so the wall clock stays consistent
// even when bubbletea is busy.
type Event struct {
	Time    time.Time
	Kind    EventKind
	Source  string
	Target  string
	Message string
	Detail  string
}

// Snapshot is the periodic summary the dashboard reads from the store. The
// fields are intentionally simple so the store can populate them from a
// single SQL query.
type Snapshot struct {
	Events     int
	Pending    int
	Delivered  int
	DeadLetter int
	SpoolBytes int64
	Uptime     time.Duration
}

// SourceState describes the runtime state of a configured source.
type SourceState struct {
	ID     string
	Type   string
	State  string // "up", "down", "reconnecting"
	Detail string
}

// DestinationState describes the runtime state of a configured destination.
type DestinationState struct {
	ID        string
	URL       string
	State     string // "idle", "delivering", "paused", "unreachable"
	Pending   int
	Delivered int
	Failed    int
}

// DashboardMsg is the envelope bubbletea routes through its Update loop. The
// concrete payload is carried by one of the fields below; the dashboard
// ignores nil fields.
type DashboardMsg struct {
	Event       *Event
	Snapshot    *Snapshot
	Source      *SourceState
	Destination *DestinationState
	Banner      string
}

// internal message types used by the bubbletea loop itself.
type tickMsg time.Time
type logTickMsg time.Time

// Dashboard is the live TUI shown while the relay daemon is running. It is
// driven entirely by Publish calls; no goroutine inside the package reads
// from the store directly. That keeps the dashboard trivially testable.
type Dashboard struct {
	out        chan DashboardMsg
	in         chan DashboardMsg
	title      string
	instance   string
	maxLog     int
	fallback   bool
	fallbackLn func(Snapshot)

	mu           sync.Mutex
	events       []Event
	snap         Snapshot
	sources      []SourceState
	destinations []DestinationState
}

// DashboardOptions configures a Dashboard.
type DashboardOptions struct {
	Title      string
	Instance   string
	MaxLog     int
	FallbackLn func(Snapshot)
}

// NewDashboard returns a Dashboard with sensible defaults. The caller is
// expected to call Run to start the loop and Publish to feed events.
func NewDashboard(opts DashboardOptions) *Dashboard {
	max := opts.MaxLog
	if max <= 0 {
		max = 200
	}
	return &Dashboard{
		out:        make(chan DashboardMsg, 256),
		in:         make(chan DashboardMsg, 256),
		title:      defaultStr(opts.Title, "LAST STATE RELAY"),
		instance:   opts.Instance,
		maxLog:     max,
		fallbackLn: opts.FallbackLn,
		fallback:   !IsTTY(),
	}
}

func defaultStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Publish hands a message to the dashboard. Publish is safe to call from
// any goroutine; the call returns immediately because the channel is
// buffered. Messages dropped under load are silently discarded.
func (d *Dashboard) Publish(msg DashboardMsg) {
	if msg.Event == nil && msg.Snapshot == nil && msg.Source == nil && msg.Destination == nil && msg.Banner == "" {
		return
	}
	select {
	case d.out <- msg:
	default:
	}
}

// Run starts the dashboard. It blocks until ctx is cancelled or the user
// presses q. When stdout is not a TTY it falls back to a line printer that
// dumps the latest snapshot every second.
func (d *Dashboard) Run(ctx context.Context) error {
	if d.fallback {
		return d.runFallback(ctx)
	}
	return d.runBubble(ctx)
}

// PublishFunc is the public closure form of Publish. The runner uses it to
// avoid exposing the dashboard type to non-UI code.
type PublishFunc func(DashboardMsg)

// Publisher returns a PublishFunc bound to this dashboard.
func (d *Dashboard) Publisher() PublishFunc { return d.Publish }

// SetFallback swaps the function used by the streaming fallback path. The
// call is useful for subcommands that want to render a custom line instead
// of the default snapshot dump. The new function only takes effect when
// stdout is not a TTY.
func (d *Dashboard) SetFallback(fn func(Snapshot)) {
	d.fallbackLn = fn
}

// runFallback streams the latest snapshot to stdout at a fixed cadence.
// It's a degraded experience, but it keeps the CLI usable over SSH and in
// CI logs.
func (d *Dashboard) runFallback(ctx context.Context) error {
	s := Styles()
	if d.fallbackLn == nil {
		fmt.Fprintln(defaultWriter(), s.AppName.Render("LAST STATE RELAY")+" — streaming mode")
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-d.out:
			d.absorb(m)
		case <-ticker.C:
			d.tick()
			if d.fallbackLn != nil {
				d.fallbackLn(d.snap)
			} else {
				d.printSnapshot()
			}
		}
	}
}

func (d *Dashboard) absorb(m DashboardMsg) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case m.Event != nil:
		d.events = append(d.events, *m.Event)
		if len(d.events) > d.maxLog {
			d.events = d.events[len(d.events)-d.maxLog:]
		}
	case m.Snapshot != nil:
		d.snap = *m.Snapshot
	case m.Source != nil:
		d.upsertSource(*m.Source)
	case m.Destination != nil:
		d.upsertDestination(*m.Destination)
	}
}

func (d *Dashboard) upsertSource(s SourceState) {
	for i, existing := range d.sources {
		if existing.ID == s.ID {
			d.sources[i] = s
			return
		}
	}
	d.sources = append(d.sources, s)
}

func (d *Dashboard) upsertDestination(s DestinationState) {
	for i, existing := range d.destinations {
		if existing.ID == s.ID {
			d.destinations[i] = s
			return
		}
	}
	d.destinations = append(d.destinations, s)
}

func (d *Dashboard) tick() {
	// The runner publishes a fresh snapshot; we just keep the previous
	// one if nothing new arrived. This avoids a "blank" line in the
	// fallback path.
}

func (d *Dashboard) printSnapshot() {
	d.mu.Lock()
	snap := d.snap
	events := append([]Event(nil), d.events...)
	sources := append([]SourceState(nil), d.sources...)
	destinations := append([]DestinationState(nil), d.destinations...)
	d.mu.Unlock()

	s := Styles()
	out := defaultWriter()
	fmt.Fprintf(out, "\n%s %s — uptime %s\n",
		s.AppName.Render("LAST STATE RELAY"),
		s.AppTag.Render("· "+d.instance),
		s.Value.Render(formatDuration(snap.Uptime)),
	)
	fmt.Fprintf(out, "  events=%s  pending=%s  delivered=%s  dead=%s  spool=%s\n",
		s.Bold.Render(fmt.Sprintf("%d", snap.Events)),
		s.Pending.Render(fmt.Sprintf("%d", snap.Pending)),
		s.OK.Render(fmt.Sprintf("%d", snap.Delivered)),
		s.Dead.Render(fmt.Sprintf("%d", snap.DeadLetter)),
		s.Info.Render(formatBytes(snap.SpoolBytes)),
	)
	if len(events) > 0 {
		last := events[len(events)-1]
		fmt.Fprintf(out, "  last: %s %s\n", s.Muted.Render(timestamp(last.Time)), last.Message)
	}
	if len(sources) > 0 {
		fmt.Fprintln(out, s.CardTitle.Render("  sources"))
		for _, src := range sources {
			fmt.Fprintf(out, "    - %s %s %s\n", src.ID, s.Muted.Render("("+src.Type+")"), stateBadge(src.State))
		}
	}
	if len(destinations) > 0 {
		fmt.Fprintln(out, s.CardTitle.Render("  destinations"))
		for _, dst := range destinations {
			fmt.Fprintf(out, "    - %s pending=%d delivered=%d failed=%d %s\n",
				dst.ID, dst.Pending, dst.Delivered, dst.Failed, stateBadge(dst.State))
		}
	}
}

// stateBadge returns a colored state string. The fallback path replaces
// the unicode glyphs with ASCII so logs stay readable in dumb terminals.
func stateBadge(state string) string {
	s := Styles()
	if !colorEnabled() {
		return "[" + strings.ToUpper(state) + "]"
	}
	switch strings.ToLower(state) {
	case "up", "idle", "ready", "delivered", "healthy":
		return s.OK.Render("● " + state)
	case "down", "failed", "dead", "unreachable":
		return s.Err.Render("● " + state)
	case "paused", "pending", "reconnecting", "delivering":
		return s.Warn.Render("● " + state)
	default:
		return s.Muted.Render("● " + state)
	}
}

// runBubble drives the bubbletea program. The model below is intentionally
// minimal: the dashboard is mostly a static layout that repaints on Tick.
func (d *Dashboard) runBubble(ctx context.Context) error {
	model := newDashboardModel(d)
	p := tea.NewProgram(model, tea.WithoutSignalHandler(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	logTick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	defer logTick.Stop()
	for {
		select {
		case <-ctx.Done():
			p.Quit()
			return nil
		case <-tick.C:
			p.Send(tickMsg(time.Now()))
		case <-logTick.C:
			p.Send(logTickMsg(time.Now()))
		case m := <-d.out:
			p.Send(m)
		case err := <-done:
			return err
		}
	}
}

// model is the bubbletea model that owns the visible state of the dashboard.
type model struct {
	parent   *Dashboard
	width    int
	height   int
	quitting bool
}

func newDashboardModel(parent *Dashboard) *model { return &model{parent: parent} }

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "c":
			m.parent.mu.Lock()
			m.parent.events = m.parent.events[:0]
			m.parent.mu.Unlock()
		}
	case DashboardMsg:
		m.parent.absorb(msg)
	case tickMsg:
		// Repaint only; the publisher decides when a snapshot changes.
	case logTickMsg:
		// Repaint so the rolling event log animates.
	}
	return m, nil
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	w := m.width
	if w < 80 {
		w = 80
	}
	h := m.height
	if h < 24 {
		h = 24
	}

	m.parent.mu.Lock()
	snap := m.parent.snap
	events := append([]Event(nil), m.parent.events...)
	sources := append([]SourceState(nil), m.parent.sources...)
	destinations := append([]DestinationState(nil), m.parent.destinations...)
	m.parent.mu.Unlock()

	header := m.headerLine(snap, w)
	stats := m.statsLine(snap, w)
	mid := m.midLine(sources, destinations, w)
	log := m.logBox(events, w, h-statsHeight-headerHeight-3)
	footer := m.footerLine(w)

	return lipgloss.JoinVertical(lipgloss.Left, header, stats, mid, log, footer)
}

const (
	headerHeight = 2
	statsHeight  = 3
)

func (m *model) headerLine(snap Snapshot, w int) string {
	s := Styles()
	left := s.AppName.Render("LAST STATE RELAY") + " " + s.AppTag.Render("· "+m.parent.instance)
	right := s.Muted.Render("uptime ") + s.Value.Render(formatDuration(snap.Uptime))
	gap := strings.Repeat(" ", max2(1, w-lipgloss.Width(left)-lipgloss.Width(right)))
	return lipgloss.NewStyle().Width(w).Render(left + gap + right)
}

func (m *model) statsLine(snap Snapshot, w int) string {
	s := Styles()
	cardWidth := (w - 6) / 4
	if cardWidth < 14 {
		cardWidth = 14
	}
	stats := []struct {
		label string
		value string
		style lipgloss.Style
	}{
		{"events", fmt.Sprintf("%d", snap.Events), s.Bold},
		{"pending", fmt.Sprintf("%d", snap.Pending), s.Pending},
		{"delivered", fmt.Sprintf("%d", snap.Delivered), s.OK},
		{"dead letter", fmt.Sprintf("%d", snap.DeadLetter), s.Dead},
	}
	cards := make([]string, 0, len(stats))
	for _, stat := range stats {
		body := s.StatLabel.Render(stat.label) + "\n" + stat.style.Render(stat.value)
		cards = append(cards, lipgloss.NewStyle().Width(cardWidth).Height(2).Render(body))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func (m *model) midLine(sources []SourceState, destinations []DestinationState, w int) string {
	leftWidth := w / 2
	rightWidth := w - leftWidth
	left := m.sourcePanel(sources, leftWidth)
	right := m.destinationPanel(destinations, rightWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(left),
		lipgloss.NewStyle().Width(rightWidth).Render(right),
	)
}

func (m *model) sourcePanel(sources []SourceState, width int) string {
	s := Styles()
	var b strings.Builder
	b.WriteString(s.CardTitle.Render("SOURCES"))
	b.WriteString("\n")
	if len(sources) == 0 {
		b.WriteString(s.Muted.Render("  (waiting for sources…)"))
	} else {
		for _, src := range sources {
			fmt.Fprintf(&b, "  %s %s %s\n", src.ID, s.Muted.Render("· "+src.Type), stateBadge(src.State))
		}
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func (m *model) destinationPanel(destinations []DestinationState, width int) string {
	s := Styles()
	var b strings.Builder
	b.WriteString(s.CardTitle.Render("DESTINATIONS"))
	b.WriteString("\n")
	if len(destinations) == 0 {
		b.WriteString(s.Muted.Render("  (no destinations configured)"))
	} else {
		for _, dst := range destinations {
			fmt.Fprintf(&b, "  %s %s\n",
				dst.ID,
				s.Muted.Render(fmt.Sprintf("p=%d d=%d f=%d %s", dst.Pending, dst.Delivered, dst.Failed, stateBadge(dst.State))),
			)
		}
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func (m *model) logBox(events []Event, w, h int) string {
	s := Styles()
	if h < 3 {
		h = 3
	}
	header := s.CardTitle.Render("EVENT LOG")
	if len(events) == 0 {
		body := s.Muted.Render("  (no events yet)")
		return lipgloss.NewStyle().Width(w).Height(h).Render(header + "\n" + body)
	}
	// Tail the last `h-1` lines.
	start := len(events) - (h - 1)
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for _, e := range events[start:] {
		b.WriteString(formatEvent(e, w))
		b.WriteString("\n")
	}
	return lipgloss.NewStyle().Width(w).Height(h).Render(b.String())
}

func (m *model) footerLine(w int) string {
	s := Styles()
	keys := []struct {
		key, label string
	}{
		{"q", "quit"},
		{"c", "clear log"},
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, s.Value.Render(k.key)+" "+s.Muted.Render(k.label))
	}
	text := strings.Join(parts, "   ")
	return lipgloss.NewStyle().Width(w).Render(text)
}

func formatEvent(e Event, w int) string {
	s := Styles()
	prefix := s.Muted.Render(timestamp(e.Time))
	badge := eventBadge(e.Kind)
	who := ""
	switch {
	case e.Source != "" && e.Target != "":
		who = s.Muted.Render(e.Source + " → " + e.Target)
	case e.Source != "":
		who = s.Muted.Render(e.Source)
	case e.Target != "":
		who = s.Muted.Render(e.Target)
	}
	msg := e.Message
	if e.Detail != "" {
		msg = msg + " " + s.Muted.Render(e.Detail)
	}
	row := fmt.Sprintf("  %s %s %s %s", prefix, badge, who, msg)
	if w > 0 && lipgloss.Width(row) > w {
		row = lipgloss.NewStyle().Width(w).Render(row)
	}
	return row
}

func eventBadge(k EventKind) string {
	s := Styles()
	if !colorEnabled() {
		switch k {
		case EventStored:
			return "[+]"
		case EventDuplicate:
			return "[=]"
		case EventDelivered:
			return "[^]"
		case EventFailed:
			return "[!]"
		case EventDead:
			return "[x]"
		case EventSourceUp, EventDestinationUp:
			return "[ON]"
		case EventSourceDown, EventDestinationDown:
			return "[OFF]"
		case EventError:
			return "[E]"
		default:
			return "[*]"
		}
	}
	switch k {
	case EventStored:
		return s.OK.Render("+ STORE")
	case EventDuplicate:
		return s.Muted.Render("= DUP")
	case EventDelivered:
		return s.OK.Render("^ DELIVER")
	case EventFailed:
		return s.Warn.Render("! RETRY")
	case EventDead:
		return s.Err.Render("x DEAD")
	case EventSourceUp:
		return s.OK.Render("● UP")
	case EventSourceDown:
		return s.Err.Render("● DOWN")
	case EventDestinationUp:
		return s.OK.Render("● READY")
	case EventDestinationDown:
		return s.Err.Render("● OFF")
	case EventError:
		return s.Err.Render("E ERROR")
	default:
		return s.Info.Render("* INFO")
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatBytes(n int64) string {
	const (
		kilobyte = 1024
		megabyte = 1024 * kilobyte
		gigabyte = 1024 * megabyte
	)
	switch {
	case n >= gigabyte:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gigabyte))
	case n >= megabyte:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(megabyte))
	case n >= kilobyte:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kilobyte))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// MustFormatBytes is the exported form of formatBytes, used by the CLI
// commands to render file sizes consistently.
func MustFormatBytes(n int64) string { return formatBytes(n) }

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}
