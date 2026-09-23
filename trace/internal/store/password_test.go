package store_test

import (
	"strings"
	"testing"

	"github.com/laststate/trace/internal/store"
)

func TestHashPasswordCheck(t *testing.T) {
	h, err := store.HashPassword("super-secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if !store.CheckPassword(h, "super-secret-password") {
		t.Fatal("check")
	}
	if store.CheckPassword(h, "wrong") {
		t.Fatal("should fail")
	}
}

func TestGeneratePasswordStrength(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		p := store.GeneratePassword()
		if len(p) < 16 {
			t.Fatalf("too short %q", p)
		}
		if seen[p] {
			t.Fatal("duplicate")
		}
		seen[p] = true
		if strings.Contains(p, "admin") {
			t.Fatal()
		}
	}
}

func TestTokenHasScope(t *testing.T) {
	tok := store.Token{Scopes: []string{"event:write"}}
	if !store.TokenHasScope(tok, "event:write") {
		t.Fatal()
	}
	if store.TokenHasScope(tok, "artifact:write") {
		t.Fatal()
	}
	tok.Scopes = []string{"project:admin"}
	if !store.TokenHasScope(tok, "anything") {
		t.Fatal()
	}
	tok.Scopes = []string{"*"}
	if !store.TokenHasScope(tok, "event:read") {
		t.Fatal()
	}
}

func TestSessionCan(t *testing.T) {
	s := store.Session{Role: "developer"}
	if !s.Can("viewer") || !s.Can("developer") {
		t.Fatal()
	}
	if s.Can("admin") {
		t.Fatal()
	}
	s.Role = "owner"
	if !s.Can("admin") {
		t.Fatal()
	}
}

func TestNormalizeSlug(t *testing.T) {
	if store.NormalizeSlug(" Hello World ") != "hello-world" {
		t.Fatal(store.NormalizeSlug(" Hello World "))
	}
}
