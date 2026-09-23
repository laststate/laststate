package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Mint creates a new token secret. Returned secret is shown once; store Hash only.
func Mint(prefixKind string) (secret, prefix string, hash []byte, err error) {
	var b [24]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", nil, err
	}
	raw := base64.RawURLEncoding.EncodeToString(b[:])
	prefix = prefixKind + "_" + raw[:8]
	secret = prefix + "." + raw[8:]
	sum := sha256.Sum256([]byte(secret))
	return secret, prefix, sum[:], nil
}

func Hash(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func Equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func PrefixOf(secret string) string {
	if i := strings.IndexByte(secret, '.'); i > 0 {
		return secret[:i]
	}
	if len(secret) >= 12 {
		return secret[:12]
	}
	return secret
}

func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Bearer(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", fmt.Errorf("missing authorization")
	}
	const p = "Bearer "
	if !strings.HasPrefix(header, p) && !strings.HasPrefix(header, "bearer ") {
		return "", fmt.Errorf("expected bearer token")
	}
	if strings.HasPrefix(header, p) {
		return strings.TrimSpace(header[len(p):]), nil
	}
	return strings.TrimSpace(header[len("bearer "):]), nil
}
