// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnvironmentAndFile(t *testing.T) {
	t.Setenv("LASTSTATE_TEST_SECRET", "  token-from-env  ")
	value, err := Resolve("env:LASTSTATE_TEST_SECRET")
	if err != nil || value != "token-from-env" {
		t.Fatalf("environment secret: value=%q err=%v", value, err)
	}

	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(" token-from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err = Resolve("file:" + path)
	if err != nil || value != "token-from-file" {
		t.Fatalf("file secret: value=%q err=%v", value, err)
	}
}

func TestResolveRejectsUnimplementedKeyring(t *testing.T) {
	if _, err := Resolve("keyring:laststate/cloud"); err == nil {
		t.Fatal("expected keyring reference to report unsupported provider")
	}
}
