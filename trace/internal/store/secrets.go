package store

import "github.com/laststate/trace/internal/secretbox"

func sealSecret(s string) string {
	if s == "" {
		return ""
	}
	return secretbox.Seal(s)
}

func openSecret(s string) string {
	if s == "" {
		return ""
	}
	out, err := secretbox.Open(s)
	if err != nil {
		return s // best-effort for misconfigured key
	}
	return out
}
