// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package symbolicate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ExternalConfig runs a sandboxed llvm-symbolizer or addr2line style tool.
type ExternalConfig struct {
	Binary  string
	Timeout time.Duration
	// Style: "llvm" (default) or "addr2line"
	Style string
}

// ResolveExternal symbolicates addresses without a shell, with timeout and output caps.
func ResolveExternal(cfg ExternalConfig, artifactPath string, addresses []uint64) ([]Frame, error) {
	if cfg.Binary == "" {
		return nil, fmt.Errorf("external symbolizer binary not configured")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	abs, err := filepath.Abs(artifactPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	var args []string
	if cfg.Style == "addr2line" {
		args = []string{"-e", abs, "-f", "-C", "-p"}
		for _, address := range addresses {
			args = append(args, fmt.Sprintf("0x%x", address))
		}
	} else {
		args = []string{"--obj=" + abs, "--functions", "--demangle", "--inlines"}
		for _, address := range addresses {
			args = append(args, fmt.Sprintf("0x%x", address))
		}
	}
	cmd := exec.CommandContext(ctx, cfg.Binary, args...) //nolint:gosec // binary path from operator config, addresses hex-encoded
	cmd.Dir = filepath.Dir(abs)
	cmd.Env = []string{"PATH=", "LANG=C"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("external symbolizer: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	const maxOut = 1 << 20
	if stdout.Len() > maxOut {
		return nil, fmt.Errorf("external symbolizer output too large")
	}
	return parseExternalOutput(addresses, stdout.String(), cfg.Style), nil
}

func parseExternalOutput(addresses []uint64, output, style string) []Frame {
	frames := make([]Frame, len(addresses))
	for i, address := range addresses {
		frames[i] = Frame{Address: address}
	}
	lines := strings.Split(output, "\n")
	idx := 0
	if style == "addr2line" {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || idx >= len(frames) {
				continue
			}
			// function at file:line
			if parts := strings.SplitN(line, " at ", 2); len(parts) == 2 {
				frames[idx].Function = Demangle(strings.TrimSpace(parts[0]))
				file, lineNo := splitFileLine(parts[1])
				frames[idx].File = file
				frames[idx].Line = lineNo
				idx++
			}
		}
		return frames
	}
	// llvm-symbolizer: function\nfile:line:col\n\n per address
	for i := 0; i < len(lines) && idx < len(frames); i++ {
		fn := strings.TrimSpace(lines[i])
		if fn == "" {
			continue
		}
		frames[idx].Function = Demangle(fn)
		if i+1 < len(lines) {
			file, lineNo, col := splitFileLineCol(strings.TrimSpace(lines[i+1]))
			frames[idx].File = file
			frames[idx].Line = lineNo
			frames[idx].Column = col
			i++
		}
		idx++
	}
	return frames
}

func splitFileLine(value string) (string, int) {
	file, line, _ := splitFileLineCol(value)
	return file, line
}

func splitFileLineCol(value string) (string, int, int) {
	parts := strings.Split(value, ":")
	if len(parts) == 1 {
		return value, 0, 0
	}
	col := 0
	line := 0
	last := parts[len(parts)-1]

	lastIsNum := false
	if _, err := strconv.Atoi(last); err == nil {
		lastIsNum = true
	}

	if lastIsNum && len(parts) >= 3 {
		// Check if second-to-last is also numeric for "file:line:col".
		if _, err := strconv.Atoi(parts[len(parts)-2]); err == nil {
			col, line = 0, 0 // Reset — handled below
		}
		// Treat last as col, second-to-last as line.
		if c, err := strconv.Atoi(last); err == nil {
			col = c
		}
		if l, err := strconv.Atoi(parts[len(parts)-2]); err == nil {
			line = l
		}
		// File is everything before last two parts.
		return strings.Join(parts[:len(parts)-2], ":"), line, col
	}

	if lastIsNum {
		// Only 2 parts: "file:line" — last is line.
		if l, err := strconv.Atoi(last); err == nil {
			line = l
		}
		return strings.Join(parts[:len(parts)-1], ":"), line, col
	}

	// Non-numeric last part: treat everything as file.
	return strings.Join(parts[:len(parts)-1], ":"), 0, 0
}
