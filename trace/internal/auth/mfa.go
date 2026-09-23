// Package auth contains MFA (TOTP) functionality.
package auth

import (
	"crypto/rand"
	"fmt"

	"github.com/pquerna/otp/totp"
)

// GenerateTOTPKey generates a new TOTP secret key for an account.
// It returns the raw secret plus the otpauth:// provisioning URI for QR codes.
func GenerateTOTPKey(account string) (secret, uri string, err error) {
	if account == "" {
		account = "user@example.com"
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "LastState",
		AccountName: account,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTPCode validates a TOTP code against a secret.
func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}

// GenerateMfaCode generates a 6-digit MFA code for email delivery.
// Uses rejection sampling over 3 bytes so every value in [100000, 999999]
// is equally likely (the previous b[0]%9000 form only produced 256 values).
func GenerateMfaCode() (string, error) {
	for {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		n := int(b[0])<<16 | int(b[1])<<8 | int(b[2]) // 0..16777215
		if n >= 16200000 {                            // 18 * 900000
			continue // reject to keep the range uniform
		}
		return fmt.Sprintf("%06d", 100000+n%900000), nil
	}
}
