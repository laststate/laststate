// Package secretbox encrypts secrets at rest (AES-256-GCM).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
)

var (
	mu   sync.RWMutex
	aead cipher.AEAD
)

// Configure sets the process-wide key. Accepts raw 32-byte, base64, or hex.
// Empty key disables encryption (Seal returns plaintext with prefix "plain:").
func Configure(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		mu.Lock()
		aead = nil
		mu.Unlock()
		return nil
	}
	raw, err := decodeKey(key)
	if err != nil {
		return err
	}
	if len(raw) != 32 {
		// derive
		sum := sha256.Sum256(raw)
		raw = sum[:]
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	mu.Lock()
	aead = g
	mu.Unlock()
	return nil
}

func decodeKey(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) >= 16 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) >= 16 {
		return b, nil
	}
	return []byte(s), nil
}

// Seal encrypts plaintext. Returns "enc:<base64>" or "plain:<text>" if no key.
func Seal(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	mu.RLock()
	g := aead
	mu.RUnlock()
	if g == nil {
		return "plain:" + plaintext
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "plain:" + plaintext
	}
	out := g.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(out)
}

// Open decrypts Seal output.
func Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if strings.HasPrefix(sealed, "plain:") {
		return strings.TrimPrefix(sealed, "plain:"), nil
	}
	if !strings.HasPrefix(sealed, "enc:") {
		// legacy plaintext secret
		return sealed, nil
	}
	mu.RLock()
	g := aead
	mu.RUnlock()
	if g == nil {
		return "", fmt.Errorf("encrypted secret but TRACE_SECRETS_KEY not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "enc:"))
	if err != nil {
		return "", err
	}
	ns := g.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := g.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
