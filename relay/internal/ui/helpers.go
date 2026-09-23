// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ansiPattern matches the ANSI escape sequences emitted by lipgloss so that
// the no-color fallback can present clean text. It deliberately keeps the
// regex small and easy to reason about.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// stripAnsi removes ANSI escape sequences from s. Used by the no-color
// fallback to keep the output readable in logs and pipes.
func stripAnsi(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// writeLine prints a single line followed by a newline to w. When color is
// disabled the line is stripped of ANSI sequences. Lines that arrive as an
// empty string are emitted as-is to preserve the caller intent.
func writeLine(w io.Writer, line string) {
	if !colorEnabled() {
		line = stripAnsi(line)
	}
	fmt.Fprintln(w, line)
}

// renderLines prints a multi-line block. Every line is treated like writeLine
// so trailing whitespace and intentional indentation survive untouched.
func renderLines(w io.Writer, block string) {
	if !colorEnabled() {
		block = stripAnsi(block)
	}
	fmt.Fprint(w, block)
	if !strings.HasSuffix(block, "\n") {
		fmt.Fprintln(w)
	}
}

// timestamp formats t as HH:MM:SS for log lines. The format is fixed because
// every caller wants the same column width in the dashboard.
func timestamp(t time.Time) string { return t.Format("15:04:05") }

// keyValue formats a "key: value" pair using the shared styles. The value is
// bolded and the key is dimmed so columns line up in monochrome terminals.
func keyValue(key, value string) string {
	s := Styles()
	return fmt.Sprintf("%s %s", s.Key.Render(key+":"), s.Value.Render(value))
}

// card builds a rounded panel with an optional title. Empty content is
// rendered as a single empty card, which keeps the dashboard grid stable.
func card(title, content string, width int) string {
	s := Styles()
	style := s.Card
	if width > 0 {
		style = style.Width(width - 2) // account for the border
	}
	body := content
	if title != "" {
		body = s.CardTitle.Render(title) + "\n" + body
	}
	return style.Render(body)
}

// joinVertical stacks blocks with a single blank line between them. The
// helper centralizes the spacing rule so that callers don't sprinkle blank
// lines around the codebase.
func joinVertical(blocks ...string) string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b) == "" {
			continue
		}
		out = append(out, b)
	}
	return strings.Join(out, "\n\n")
}

// joinHorizontal places two blocks side by side. When color is disabled the
// caller receives a vertical join instead so the output stays readable in
// narrow terminals.
func joinHorizontal(left, right string, leftWidth, rightWidth int) string {
	if !colorEnabled() {
		return left + "\n" + right
	}
	cols := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(leftWidth).Render(left), right)
	return cols
}

// divider returns a horizontal rule that matches the terminal width when
// color is enabled, or a string of dashes otherwise.
func divider(width int) string {
	s := Styles()
	if !colorEnabled() {
		return strings.Repeat("-", width)
	}
	return s.Divider.Render(strings.Repeat("─", width))
}
