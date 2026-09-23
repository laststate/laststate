// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ui

import (
	"fmt"
	"io"
	"os"
	"time"
)

// LogLevel categorises a log line.
type LogLevel int

const (
	LogInfo LogLevel = iota
	LogOK
	LogWarn
	LogErr
)

// Logf prints a single timestamped line. The format is fixed because the
// dashboard and the streaming fallback both rely on a stable column width.
func Logf(w io.Writer, level LogLevel, format string, args ...any) {
	if w == nil {
		w = os.Stdout
	}
	if level == LogWarn || level == LogErr {
		if w == os.Stdout {
			w = os.Stderr
		}
	}
	prefix := logPrefix(level)
	timestamp := Styles().Muted.Render(time.Now().Format("15:04:05"))
	body := fmt.Sprintf(format, args...)
	if !colorEnabled() {
		body = stripAnsi(body)
		prefix = stripAnsi(prefix)
	}
	fmt.Fprintf(w, "%s %s %s\n", timestamp, prefix, body)
}

func logPrefix(level LogLevel) string {
	switch level {
	case LogOK:
		return Icon("ok")
	case LogWarn:
		return Icon("warn")
	case LogErr:
		return Icon("err")
	default:
		return Icon("info")
	}
}
