// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
)

// ProgressOptions describes a progress bar session.
type ProgressOptions struct {
	Title   string // text shown on the left of the bar
	Total   int    // total units to process; 0 means "unknown, show percent 0"
	Width   int    // bar width in columns; 0 picks a sensible default
	Started time.Time
}

// Progress is an animated progress bar. It writes to stdout in raw mode so
// the bar can redraw itself in place. Callers should call Finish exactly
// once. The zero value is not usable; use NewProgress.
type Progress struct {
	opts    ProgressOptions
	bar     progress.Model
	current int
	closed  bool
	out     io.Writer
}

// NewProgress returns a configured progress bar. The bar immediately writes
// a header line and an empty bar to the terminal.
func NewProgress(out io.Writer, opts ProgressOptions) *Progress {
	if out == nil {
		out = defaultWriter()
	}
	if opts.Width <= 0 {
		opts.Width = 40
	}
	if opts.Started.IsZero() {
		opts.Started = time.Now()
	}
	s := Styles()
	bar := progress.New(
		progress.WithGradient(string(s.p.brandAlt), string(s.p.brand)),
		progress.WithoutPercentage(),
		progress.WithWidth(opts.Width),
	)
	return &Progress{opts: opts, bar: bar, out: out}
}

// Set advances the bar to current units. Negative or oversized values are
// clamped to [0, total]. If total is 0 the bar is rendered in indeterminate
// mode (a pulsing block) by the time Set is called repeatedly.
func (p *Progress) Set(current int) {
	if p.closed {
		return
	}
	if p.opts.Total > 0 && current > p.opts.Total {
		current = p.opts.Total
	}
	if current < 0 {
		current = 0
	}
	p.current = current

	ratio := 0.0
	if p.opts.Total > 0 {
		ratio = float64(current) / float64(p.opts.Total)
	} else {
		// Pulse between 0 and 1 based on the millisecond clock so the
		// bar still feels alive when the total is unknown.
		phase := float64(time.Since(p.opts.Started).Milliseconds()%2000) / 2000.0
		if phase > 0.5 {
			phase = 1 - phase
		}
		ratio = phase
	}

	frame := p.frame(ratio)
	if _, err := fmt.Fprint(p.out, frame); err != nil {
		return
	}
}

// SetMessage changes the title shown on the left of the bar. The bar is
// redrawn in place so the change is seamless.
func (p *Progress) SetMessage(title string) {
	if p.closed {
		return
	}
	p.opts.Title = title
}

// Finish closes the bar and prints a final newline. The supplied message is
// printed on the line below the bar in the "ok" style. Subsequent calls
// are no-ops.
func (p *Progress) Finish(message string) {
	if p.closed {
		return
	}
	p.closed = true
	if message == "" {
		message = "done"
	}
	// Move to a fresh line and emit a confirmation.
	fmt.Fprintln(p.out)
	Success(p.out, fmt.Sprintf("%s — %s", p.opts.Title, message))
}

// Fail closes the bar and prints the supplied error in the "err" style.
func (p *Progress) Fail(err error) {
	if p.closed {
		return
	}
	p.closed = true
	fmt.Fprintln(p.out)
	if err == nil {
		Error(p.out, p.opts.Title+" failed")
		return
	}
	Error(p.out, fmt.Sprintf("%s — %s", p.opts.Title, err.Error()))
}

func (p *Progress) frame(ratio float64) string {
	if !colorEnabled() {
		return plainFrame(p.opts.Title, ratio, p.opts.Total, p.current, p.opts.Width)
	}
	bar := p.bar.ViewAs(ratio)
	s := Styles()
	title := s.Bold.Render(p.opts.Title)
	pct := s.Muted.Render(percent(ratio, p.opts.Total > 0))
	return fmt.Sprintf("\r\x1b[2K%s %s %s", title, bar, pct)
}

func percent(ratio float64, known bool) string {
	if !known {
		return "···"
	}
	return fmt.Sprintf("%3d%%", int(ratio*100))
}

func plainFrame(title string, ratio float64, total, current, width int) string {
	filled := int(ratio * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "#"
		} else {
			bar += "-"
		}
	}
	pct := percent(ratio, total > 0)
	if total > 0 {
		return fmt.Sprintf("\r\x1b[2K%s [%s] %s (%d/%d)", title, bar, pct, current, total)
	}
	return fmt.Sprintf("\r\x1b[2K%s [%s] %s", title, bar, pct)
}

// defaultWriter returns the package's standard output stream. The function
// exists so the Progress type can be tested with a buffer.
func defaultWriter() io.Writer { return os.Stdout }
