// Package fingerprint builds stable v1 issue fingerprints.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

const Version = "v1"

func Compute(d decode.Decoded, report analysis.Report) string {
	parts := []string{
		Version,
		lep.EventTypeName(d.Header.Type),
		lep.ArchName(d.Header.Architecture),
		report.ProbableCause,
	}
	if len(report.Frames) > 0 {
		f := report.Frames[0]
		if f.Function != "" {
			parts = append(parts, "fn:"+normalize(f.Function))
		} else {
			parts = append(parts, fmt.Sprintf("pc:0x%x", f.Address&^1))
		}
		if f.File != "" {
			parts = append(parts, "file:"+normalize(f.File))
			if f.Line > 0 {
				parts = append(parts, fmt.Sprintf("line:%d", f.Line))
			}
		}
	} else if d.PC != 0 {
		parts = append(parts, fmt.Sprintf("pc:0x%x", d.PC&^1))
	}
	if d.Assert != "" {
		parts = append(parts, "assert:"+normalize(d.Assert))
	}
	// Optional device-emitted fingerprint from EVENT TLV.
	if d.Event != nil && d.Event.Fingerprint != 0 {
		parts = append(parts, fmt.Sprintf("devfp:0x%x", d.Event.Fingerprint))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return Version + ":" + hex.EncodeToString(sum[:16])
}

func Title(d decode.Decoded, report analysis.Report) string {
	if report.ProbableCause != "" {
		return report.ProbableCause
	}
	if len(report.Frames) > 0 && report.Frames[0].Function != "" {
		return report.Frames[0].Function
	}
	if d.PC != 0 {
		return fmt.Sprintf("%s at 0x%08x", lep.EventTypeName(d.Header.Type), d.PC)
	}
	return lep.EventTypeName(d.Header.Type)
}

func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\\", "/")
	// strip path prefix noise
	if i := strings.LastIndex(s, "/"); i >= 0 && !strings.Contains(s, "(") {
		// keep full function names; only strip pure file paths handled by caller
	}
	return strings.ToLower(s)
}
