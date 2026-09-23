package auth_test

import (
	"strings"
	"testing"

	"github.com/laststate/trace/internal/auth"
)

func TestMintHashRoundTrip(t *testing.T) {
	secret, prefix, hash, err := auth.Mint("lst_ingest")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, prefix+".") {
		t.Fatalf("secret %q prefix %q", secret, prefix)
	}
	if !auth.Equal(hash, auth.Hash(secret)) {
		t.Fatal("hash mismatch")
	}
	if auth.Equal(hash, auth.Hash(secret+"x")) {
		t.Fatal("different secret should not match")
	}
	if auth.PrefixOf(secret) != prefix {
		t.Fatalf("PrefixOf=%q want %q", auth.PrefixOf(secret), prefix)
	}
}

func TestMintUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, _, _, err := auth.Mint("lst_sess")
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatal("duplicate secret")
		}
		seen[s] = true
	}
}

func TestBearer(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", true},
		{"Basic x", "", true},
		{"Bearer tok.en", "tok.en", false},
		{"bearer tok.en", "tok.en", false},
		{"Bearer   spaced  ", "spaced", false},
	}
	for _, tc := range cases {
		got, err := auth.Bearer(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q expected err", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%q got %q err=%v", tc.in, got, err)
		}
	}
}

func TestEqualLengthMismatch(t *testing.T) {
	if auth.Equal([]byte{1}, []byte{1, 2}) {
		t.Fatal("length mismatch should be false")
	}
	if !auth.Equal([]byte{1, 2}, []byte{1, 2}) {
		t.Fatal("equal")
	}
}

func TestRandomHex(t *testing.T) {
	h := auth.RandomHex(16)
	if len(h) != 32 {
		t.Fatalf("len=%d", len(h))
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex %q", h)
		}
	}
}

func TestPrefixOfFallbacks(t *testing.T) {
	if auth.PrefixOf("short") != "short" {
		t.Fatal("short")
	}
	if p := auth.PrefixOf("abcdefghijklm"); len(p) != 12 {
		t.Fatalf("prefix12=%q", p)
	}
}
