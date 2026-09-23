package secretbox

import (
	"testing"
)

func TestConfigureAndSealOpen(t *testing.T) {
	key := "abcdefghijklmnopqrstuvwxyz123456" // 32 bytes
	if err := Configure(key); err != nil {
		t.Fatalf("configure should not error: %v", err)
	}
	defer Configure("")

	sealed := Seal("hello world")
	if sealed == "" {
		t.Fatal("sealed should not be empty")
	}
	if len(sealed) <= 5 { // "enc:" prefix
		t.Error("sealed should be longer than prefix")
	}

	plain, err := Open(sealed)
	if err != nil {
		t.Fatalf("open should not error: %v", err)
	}
	if plain != "hello world" {
		t.Errorf("expected 'hello world', got %q", plain)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef" // 32 bytes hex
	if err := Configure(key); err != nil {
		t.Fatalf("configure: %v", err)
	}
	defer Configure("")

	tests := []string{
		"",
		"a",
		"short",
		"this is a longer string that should still work with aes-gcm",
		"special chars: !@#$%^&*()",
		"unicode: こんにちは世界",
	}
	for _, tc := range tests {
		sealed := Seal(tc)
		plain, err := Open(sealed)
		if err != nil {
			t.Errorf("roundtrip failed for %q: %v", tc, err)
			continue
		}
		if plain != tc {
			t.Errorf("roundtrip mismatch: expected %q, got %q", tc, plain)
		}
	}
}

func TestSealWithoutKey(t *testing.T) {
	Configure("") // ensure no key

	sealed := Seal("hello")
	if sealed != "plain:hello" {
		t.Errorf("expected plain:hello, got %q", sealed)
	}

	plain, err := Open(sealed)
	if err != nil {
		t.Fatalf("open plain text should not error: %v", err)
	}
	if plain != "hello" {
		t.Errorf("expected hello, got %q", plain)
	}
}

func TestOpenEncryptedWithoutKey(t *testing.T) {
	Configure("") // no key

	// Create an encrypted string with a key, then try to open without it
	Configure("abcdefghijklmnopqrstuvwxyz123456")
	sealed := Seal("secret data")
	Configure("") // remove key

	_, err := Open(sealed)
	if err == nil {
		t.Error("expected error when opening encrypted data without key")
	}
}

func TestOpenPlainPrefix(t *testing.T) {
	Configure("")

	plain, err := Open("plain:some value")
	if err != nil {
		t.Fatalf("open plain: prefix should not error: %v", err)
	}
	if plain != "some value" {
		t.Errorf("expected 'some value', got %q", plain)
	}
}

func TestOpenLegacyPlaintext(t *testing.T) {
	Configure("")

	// Legacy: no prefix at all
	plain, err := Open("legacy-plaintext-value")
	if err != nil {
		t.Fatalf("legacy plaintext should not error: %v", err)
	}
	if plain != "legacy-plaintext-value" {
		t.Errorf("expected legacy-plaintext-value, got %q", plain)
	}
}

func TestDecodeKeyFormats(t *testing.T) {
	// Test hex key
	if err := Configure("0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("hex key should work: %v", err)
	}

	// Test base64 key (32 bytes base64)
	if err := Configure("YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY="); err != nil {
		t.Fatalf("base64 key should work: %v", err)
	}

	// Test raw key
	if err := Configure("abcdefghijklmnopqrstuvwxyz123456"); err != nil {
		t.Fatalf("raw key should work: %v", err)
	}

	// Test short key (should derive via SHA256)
	if err := Configure("short"); err != nil {
		t.Fatalf("short key should derive: %v", err)
	}

	Configure("")
}

func TestSealEmpty(t *testing.T) {
	Configure("")
	sealed := Seal("")
	if sealed != "" {
		t.Errorf("expected empty, got %q", sealed)
	}
}

func TestOpenEmpty(t *testing.T) {
	Configure("")
	plain, err := Open("")
	if err != nil {
		t.Fatalf("open empty should not error: %v", err)
	}
	if plain != "" {
		t.Errorf("expected empty, got %q", plain)
	}
}

func TestDifferentNoncesProduceDifferentCiphertext(t *testing.T) {
	key := "abcdefghijklmnopqrstuvwxyz123456"
	if err := Configure(key); err != nil {
		t.Fatalf("configure: %v", err)
	}
	defer Configure("")

	sealed1 := Seal("same plaintext")
	sealed2 := Seal("same plaintext")
	if sealed1 == sealed2 {
		t.Error("two seals of same plaintext should produce different ciphertext (nonce randomization)")
	}
}
