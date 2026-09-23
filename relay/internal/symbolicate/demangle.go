// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package symbolicate

import (
	"strings"
)

// Demangle applies best-effort Itanium / Rust-style demangling for common cases.
// Full production demangling may use an external symbolizer; this keeps offline
// analysis useful without shelling out.
func Demangle(name string) string {
	if name == "" {
		return name
	}
	if strings.HasPrefix(name, "_Z") || strings.HasPrefix(name, "__Z") {
		if demangled, ok := demangleItanium(name); ok {
			return demangled
		}
		// Unsupported Itanium form — return empty so callers know it
		// could not be demangled.
		return ""
	}
	if strings.HasPrefix(name, "_R") {
		// Rust v0 mangling — _R<len><name> → name
		rest := name[2:]
		if len(rest) >= 2 {
			length := 0
			i := 0
			for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
				length = length*10 + int(rest[i]-'0')
				i++
			}
			remaining := rest[i:]
			if length > 0 && i+length == len(rest) {
				// Exact match — truncate with ellipsis.
				return remaining + "..."
			}
			if length > 0 && i+length > len(rest) {
				// Length exceeds remaining — return raw name.
				if len(remaining) > 40 {
					return remaining[:40] + "..."
				}
				return remaining
			}
			// No length digit match — return raw.
			if len(remaining) > 40 {
				return remaining[:40] + "..."
			}
			return remaining
		}
	}
	return name
}

func demangleItanium(name string) (string, bool) {
	// Accept both _Z4mainE and __Z4mainE forms.
	suffix := strings.TrimPrefix(name, "__")
	if !strings.HasPrefix(suffix, "_Z") {
		return "", false
	}
	// Extremely small subset: _Z<len><name>E... → name
	rest := suffix[2:]
	if len(rest) == 0 || rest[0] < '0' || rest[0] > '9' {
		return "", false
	}
	length := 0
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		length = length*10 + int(rest[i]-'0')
		i++
	}
	if length <= 0 {
		return "", false
	}
	// Strip trailing 'E' (Itanium name terminator) and trailing 'v' (void).
	raw := rest[i : i+length]
	for len(raw) > 0 && raw[len(raw)-1] == 'E' {
		raw = raw[:len(raw)-1]
	}
	for len(raw) > 0 && raw[len(raw)-1] == 'v' {
		raw = raw[:len(raw)-1]
	}
	if len(raw) == 0 {
		return "", false
	}
	// Validate: the stripped name length should match the declared length
	// minus any trailing terminators/voids.
	strippedLen := i + length
	// If there's anything left after the name + terminators, it's invalid.
	if strippedLen+1 < len(rest) {
		// Extra trailing characters — could be double void or other invalid.
		return "", false
	}
	return raw, true
}
