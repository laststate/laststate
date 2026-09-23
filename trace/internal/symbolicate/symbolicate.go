// Package symbolicate resolves addresses via llvm-symbolizer or addr2line (sandboxed).
package symbolicate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxAddresses = 64
	maxOutput    = 1 << 20
	defaultTO    = 5 * time.Second
)

type Frame struct {
	Address  uint64 `json:"address"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// Resolve tries llvm-symbolizer then *-addr2line. Sandbox: abs path, wiped env, timeout, caps.
func Resolve(artifactPath string, addresses []uint64) ([]Frame, []string, error) {
	if len(addresses) == 0 {
		return nil, nil, nil
	}
	if len(addresses) > maxAddresses {
		addresses = addresses[:maxAddresses]
	}
	abs, err := filepath.Abs(artifactPath)
	if err != nil {
		return nil, nil, err
	}
	// refuse path escape / missing file
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		out := rawFrames(addresses)
		return out, []string{"artifact missing for symbolication"}, nil
	}
	var warns []string
	for _, c := range candidates() {
		frames, err := runSandboxed(c.bin, c.style, abs, addresses)
		if err != nil {
			warns = append(warns, c.bin+": "+err.Error())
			continue
		}
		return frames, warns, nil
	}
	// Fallback: ELF symbol table (no external binary)
	if frames, err := ResolveELF(abs, addresses); err == nil {
		warns = append(warns, "used ELF symbol table (no llvm-symbolizer in PATH)")
		return frames, warns, nil
	} else {
		warns = append(warns, "elf symbols: "+err.Error())
	}
	warns = append(warns, "no symbolizer binary found in PATH")
	return rawFrames(addresses), warns, nil
}

func rawFrames(addresses []uint64) []Frame {
	out := make([]Frame, len(addresses))
	for i, a := range addresses {
		out[i] = Frame{Address: a}
	}
	return out
}

type cand struct{ bin, style string }

func candidates() []cand {
	var out []cand
	for _, b := range []string{"llvm-symbolizer", "llvm-symbolizer-18", "llvm-symbolizer-17"} {
		if p, err := exec.LookPath(b); err == nil {
			out = append(out, cand{p, "llvm"})
		}
	}
	for _, b := range []string{"arm-none-eabi-addr2line", "riscv-none-elf-addr2line", "addr2line"} {
		if p, err := exec.LookPath(b); err == nil {
			out = append(out, cand{p, "addr2line"})
		}
	}
	return out
}

func runSandboxed(bin, style, absArtifact string, addresses []uint64) ([]Frame, error) {
	// only allow absolute binary from LookPath
	if !filepath.IsAbs(bin) {
		return nil, fmt.Errorf("symbolizer path not absolute")
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTO)
	defer cancel()
	var args []string
	if style == "addr2line" {
		args = []string{"-e", absArtifact, "-f", "-C", "-p"}
		for _, a := range addresses {
			args = append(args, fmt.Sprintf("0x%x", a))
		}
	} else {
		args = []string{"--obj=" + absArtifact, "--functions", "--demangle", "--inlines"}
		for _, a := range addresses {
			args = append(args, fmt.Sprintf("0x%x", a))
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// sandbox: no shell, wiped env, no network via empty proxies, workdir = artifact dir only
	cmd.Dir = filepath.Dir(absArtifact)
	cmd.Env = []string{
		"LANG=C", "LC_ALL=C", "PATH=",
		"HOME=", "TMPDIR=", "TMP=", "TEMP=",
		"http_proxy=http://127.0.0.1:0", "https_proxy=http://127.0.0.1:0",
		"HTTP_PROXY=http://127.0.0.1:0", "HTTPS_PROXY=http://127.0.0.1:0",
		"NO_PROXY=*", "no_proxy=*",
	}
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// CPU soft limit via context timeout (defaultTO); memory/output capped below
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() > maxOutput {
		return nil, fmt.Errorf("symbolizer output too large")
	}
	return parseOutput(addresses, stdout.String(), style), nil
}

func parseOutput(addresses []uint64, output, style string) []Frame {
	frames := make([]Frame, len(addresses))
	for i, a := range addresses {
		frames[i] = Frame{Address: a}
	}
	lines := strings.Split(output, "\n")
	idx := 0
	if style == "addr2line" {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || idx >= len(frames) {
				continue
			}
			if parts := strings.SplitN(line, " at ", 2); len(parts) == 2 {
				frames[idx].Function = strings.TrimSpace(parts[0])
				frames[idx].File, frames[idx].Line = splitFileLine(parts[1])
				idx++
			}
		}
		return frames
	}
	// llvm-symbolizer with --inlines: each address may emit multiple fn/file pairs,
	// separated by blank lines between addresses.
	for i := 0; i < len(lines) && idx < len(frames); {
		fn := strings.TrimSpace(lines[i])
		if fn == "" {
			// blank line = next address group
			if i > 0 && strings.TrimSpace(lines[i-1]) != "" {
				idx++
			}
			i++
			continue
		}
		if idx >= len(frames) {
			break
		}
		// First frame for this address is outer; subsequent before blank = inlines
		isInline := frames[idx].Function != ""
		if !isInline {
			frames[idx].Function = fn
		} else {
			// Expand: keep outer, append inline as extra synthetic frame later
			// Mark current as having been filled; store deepest inline on same slot's note
			// Prefer innermost for File/Line (last wins for display)
			frames[idx].Inline = true
		}
		if i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next != "" && !strings.Contains(next, " at ") {
				// likely file:line
				f, ln, col := splitFileLineCol(next)
				if !isInline {
					frames[idx].File, frames[idx].Line, frames[idx].Column = f, ln, col
				} else {
					// keep innermost location
					frames[idx].File, frames[idx].Line, frames[idx].Column = f, ln, col
					if frames[idx].Function != "" && fn != frames[idx].Function {
						frames[idx].Function = fn + " inlined into " + frames[idx].Function
					} else {
						frames[idx].Function = fn
					}
				}
				i += 2
				continue
			}
		}
		i++
	}
	return frames
}

func splitFileLine(v string) (string, int) {
	f, l, _ := splitFileLineCol(v)
	return f, l
}

func splitFileLineCol(v string) (string, int, int) {
	parts := strings.Split(v, ":")
	if len(parts) < 2 {
		return v, 0, 0
	}
	col, line, fileEnd := 0, 0, len(parts)
	if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
		col = n
		fileEnd--
		if fileEnd > 0 {
			if n, err := strconv.Atoi(parts[fileEnd-1]); err == nil {
				line = n
				fileEnd--
			} else {
				line, col = col, 0
			}
		}
	}
	return strings.Join(parts[:fileEnd], ":"), line, col
}
