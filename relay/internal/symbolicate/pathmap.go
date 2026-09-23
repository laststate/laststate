// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package symbolicate

import "strings"

// PathMap rewrites source paths from DWARF into operator-visible paths.
type PathMap struct {
	Prefix string
	Maps   []PathRewrite
}

type PathRewrite struct {
	From string
	To   string
}

func (m PathMap) Rewrite(path string) string {
	if path == "" {
		return path
	}
	normalized := filepathToSlash(path)
	for _, rule := range m.Maps {
		from := filepathToSlash(rule.From)
		if from == "" {
			continue
		}
		if strings.HasPrefix(normalized, from) {
			return filepathToSlash(rule.To) + strings.TrimPrefix(normalized, from)
		}
	}
	if m.Prefix != "" {
		prefix := filepathToSlash(m.Prefix)
		if strings.HasPrefix(normalized, prefix) {
			return strings.TrimPrefix(normalized, prefix)
		}
	}
	return normalized
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
