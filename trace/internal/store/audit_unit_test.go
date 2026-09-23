package store

import (
	"testing"
)

func TestComputeEntryHash_Deterministic(t *testing.T) {
	// Same inputs produce same hash.
	h1 := computeEntryHash("prev1", "user.login", "user", "u_123", `{"ip":"127.0.0.1"}`)
	h2 := computeEntryHash("prev1", "user.login", "user", "u_123", `{"ip":"127.0.0.1"}`)
	if h1 != h2 {
		t.Fatalf("non-deterministic: %s vs %s", h1, h2)
	}
	if h1 == "" {
		t.Fatal("empty hash")
	}
	// 64 hex chars (SHA-256).
	if len(h1) != 64 {
		t.Fatalf("hash length: got %d want 64", len(h1))
	}
}

func TestComputeEntryHash_ChangesOnAnyInput(t *testing.T) {
	base := computeEntryHash("", "a", "b", "c", "{}")
	cases := map[string]string{
		"prev":      computeEntryHash("x", "a", "b", "c", "{}"),
		"action":    computeEntryHash("", "A", "b", "c", "{}"),
		"target":    computeEntryHash("", "a", "B", "c", "{}"),
		"target_id": computeEntryHash("", "a", "b", "C", "{}"),
		"metadata":  computeEntryHash("", "a", "b", "c", `{"x":1}`),
	}
	for k, v := range cases {
		if v == base {
			t.Errorf("hash unchanged when %s changes", k)
		}
	}
}

func TestComputeEntryHash_EmptyPrevIsGenesis(t *testing.T) {
	// The genesis row uses prev="" — make sure it doesn't crash and produces
	// a stable hash.
	h := computeEntryHash("", "system.init", "", "", "{}")
	if h == "" {
		t.Fatal("genesis hash empty")
	}
}
