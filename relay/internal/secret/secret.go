// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package secret resolves env:/file: credential references for configuration.
package secret

import (
	"fmt"
	"os"
	"strings"
)

// Resolve expands a supported secret reference. Plain values remain supported
// for local development, but production configurations should use env: or
// file: references so credentials never need to be copied into the database.
func Resolve(reference string) (string, error) {
	switch {
	case reference == "":
		return "", nil
	case strings.HasPrefix(reference, "env:"):
		name := strings.TrimSpace(strings.TrimPrefix(reference, "env:"))
		if name == "" {
			return "", fmt.Errorf("empty environment variable secret reference")
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		return strings.TrimSpace(value), nil
	case strings.HasPrefix(reference, "file:"):
		path := strings.TrimSpace(strings.TrimPrefix(reference, "file:"))
		if path == "" {
			return "", fmt.Errorf("empty file secret reference")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	case strings.HasPrefix(reference, "keyring:"):
		return "", fmt.Errorf("keyring secret references require a platform keyring provider; use env: or file: in this build")
	default:
		return reference, nil
	}
}
