// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Icon returns the unicode glyph used as a semantic prefix. The fallback path
// substitutes an ASCII variant so the output stays legible in dumb terminals.
func Icon(kind string) string {
	s := Styles()
	if !colorEnabled() {
		switch kind {
		case "ok":
			return "[OK]"
		case "warn":
			return "[!]"
		case "err":
			return "[X]"
		case "info":
			return "[i]"
		case "pending":
			return "[~]"
		case "dead":
			return "[!]"
		default:
			return "[ ]"
		}
	}
	switch kind {
	case "ok":
		return s.OK.Render("✓")
	case "warn":
		return s.Warn.Render("⚠")
	case "err":
		return s.Err.Render("✗")
	case "info":
		return s.Info.Render("ⓘ")
	case "pending":
		return s.Pending.Render("◌")
	case "dead":
		return s.Dead.Render("✗")
	default:
		return " "
	}
}

// Success prints an "ok" line to w. Use it for confirmations: "stored x.lep",
// "configuration valid", "spool healthy".
func Success(w io.Writer, msg string) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(w, "%s %s\n", Icon("ok"), msg)
}

// Info prints an informational line. Use it for hints and process narration
// that does not require action from the user.
func Info(w io.Writer, msg string) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(w, "%s %s\n", Icon("info"), msg)
}

// Warn prints a warning line. Use it when something is unexpected but the
// command can still proceed.
func Warn(w io.Writer, msg string) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "%s %s\n", Icon("warn"), msg)
}

// Error prints an error line. Use it for terminal failures; non-terminal
// helpers should return the error to the caller instead.
func Error(w io.Writer, msg string) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "%s %s\n", Icon("err"), msg)
}

// Pending prints a "pending" line. Use it for work-in-progress statements
// that need a neutral visual treatment (the spinner covers the same need
// when the work is animated).
func Pending(w io.Writer, msg string) {
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(w, "%s %s\n", Icon("pending"), msg)
}

// KVPair formats a "key: value" pair with a dimmed key. Centralized here so
// all CLI commands line up the same way.
func KVPair(key, value string) string { return keyValue(key, value) }

// Card wraps content inside a rounded panel. Pass an empty title to get a
// plain card. The function respects color settings and degrades to the raw
// content when color is disabled.
func Card(title, content string, width int) string { return card(title, content, width) }

// Divider returns a horizontal rule of the requested width.
func Divider(width int) string { return divider(width) }

// JoinVertical stacks blocks with a blank line between non-empty ones.
func JoinVertical(blocks ...string) string { return joinVertical(blocks...) }

// JoinHorizontal lays two blocks side by side. Falls back to a vertical
// layout when color is disabled.
func JoinHorizontal(left, right string, leftWidth, rightWidth int) string {
	return joinHorizontal(left, right, leftWidth, rightWidth)
}

// Indent prefixes every line in s with the given number of spaces. Used by
// the table helpers to align content under a section header.
func Indent(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
