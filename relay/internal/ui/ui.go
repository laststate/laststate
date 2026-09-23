// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package ui provides styled terminal output for the laststate-relay CLI.
//
// The package is built on top of lipgloss and bubbletea and exposes a small,
// opinionated API surface: a banner, semantic status printers, animated
// spinners, progress bars, sectioned panels, and a live TUI dashboard for the
// `run` daemon. Every public helper degrades to plain text when stdout is not
// a TTY, so the CLI keeps working in CI, scripts, and pipes.
package ui

import (
	"os"
	"sync/atomic"

	"golang.org/x/term"
)

// ForceColor overrides the auto-detected color decision. When nil the package
// inspects the terminal and the NO_COLOR environment variable itself.
var ForceColor atomic.Pointer[bool]

// colorEnabled reports whether the package should emit ANSI color sequences.
// The decision is cached after the first call so that helpers stay cheap.
func colorEnabled() bool {
	if v := ForceColor.Load(); v != nil {
		return *v
	}
	if _, present := os.LookupEnv("NO_COLOR"); present {
		return false
	}
	fd := int(os.Stdout.Fd())
	return term.IsTerminal(fd)
}

// IsTTY reports whether stdout is attached to a terminal. Callers use it to
// pick between the live TUI dashboard and the line-based fallback logger.
func IsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
