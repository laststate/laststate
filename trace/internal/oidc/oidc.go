// Package oidc implements OIDC authorization-code login with PKCE, nonce, and ID token checks.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes default openid email profile
	Scopes string
}

func (c Config) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.RedirectURL != ""
}

type Provider struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	UserURL  string `json:"userinfo_endpoint"`
	JWKSURL  string `json:"jwks_uri"`
	EndURL   string `json:"end_session_endpoint"`
}

func Discover(ctx context.Context, issuer string) (Provider, error) {
	u := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Provider{}, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Provider{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Provider{}, fmt.Errorf("oidc discovery %s", resp.Status)
	}
	var p Provider
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return Provider{}, err
	}
	if p.AuthURL == "" || p.TokenURL == "" {
		return Provider{}, fmt.Errorf("oidc discovery incomplete")
	}
	if p.Issuer == "" {
		p.Issuer = strings.TrimRight(issuer, "/")
	}
	return p, nil
}

// AuthCodeURL builds authorize URL with state, nonce, and PKCE S256.
func (c Config) AuthCodeURL(p Provider, state, nonce, codeChallenge string) string {
	scopes := c.Scopes
	if scopes == "" {
		scopes = "openid email profile"
	}
	v := url.Values{}
	v.Set("client_id", c.ClientID)
	v.Set("redirect_uri", c.RedirectURL)
	v.Set("response_type", "code")
	v.Set("scope", scopes)
	v.Set("state", state)
	if nonce != "" {
		v.Set("nonce", nonce)
	}
	if codeChallenge != "" {
		v.Set("code_challenge", codeChallenge)
		v.Set("code_challenge_method", "S256")
	}
	return p.AuthURL + "?" + v.Encode()
}

// S256Challenge returns base64url(SHA256(verifier)) without padding.
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type TokenResp struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

type UserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

type IDClaims struct {
	Iss           string `json:"iss"`
	Sub           string `json:"sub"`
	Aud           any    `json:"aud"`
	Exp           int64  `json:"exp"`
	Iat           int64  `json:"iat"`
	Nonce         string `json:"nonce"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

func (c Config) Exchange(ctx context.Context, p Provider, code, codeVerifier string) (TokenResp, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURL)
	form.Set("client_id", c.ClientID)
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
	}
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResp{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TokenResp{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return TokenResp{}, fmt.Errorf("token exchange: %s %s", resp.Status, string(body))
	}
	var tr TokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return TokenResp{}, err
	}
	return tr, nil
}

// ValidateIDToken verifies RS256 signature via JWKS, iss, aud, exp, and nonce.
func (c Config) ValidateIDToken(ctx context.Context, p Provider, idToken, expectedNonce string) (IDClaims, error) {
	if idToken == "" {
		return IDClaims{}, fmt.Errorf("missing id_token")
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return IDClaims{}, fmt.Errorf("malformed jwt")
	}
	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return IDClaims{}, fmt.Errorf("bad jwt header")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(hdrJSON, &hdr); err != nil {
		return IDClaims{}, err
	}
	if hdr.Alg != "RS256" {
		return IDClaims{}, fmt.Errorf("unsupported alg %s", hdr.Alg)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return IDClaims{}, fmt.Errorf("bad jwt payload")
	}
	var claims IDClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return IDClaims{}, err
	}
	wantIss := strings.TrimRight(p.Issuer, "/")
	gotIss := strings.TrimRight(claims.Iss, "/")
	if gotIss != wantIss {
		return IDClaims{}, fmt.Errorf("issuer mismatch")
	}
	if !audienceContains(claims.Aud, c.ClientID) {
		return IDClaims{}, fmt.Errorf("audience mismatch")
	}
	now := time.Now().Unix()
	if claims.Exp == 0 || claims.Exp < now-60 {
		return IDClaims{}, fmt.Errorf("token expired")
	}
	if claims.Iat > now+120 {
		return IDClaims{}, fmt.Errorf("token issued in future")
	}
	if expectedNonce != "" {
		if claims.Nonce == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
			return IDClaims{}, fmt.Errorf("nonce mismatch")
		}
	}
	if p.JWKSURL == "" {
		return IDClaims{}, fmt.Errorf("no jwks_uri for signature verification")
	}
	pub, err := fetchRSAKey(ctx, p.JWKSURL, hdr.Kid)
	if err != nil {
		return IDClaims{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return IDClaims{}, fmt.Errorf("bad signature encoding")
	}
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sig); err != nil {
		return IDClaims{}, fmt.Errorf("invalid signature: %w", err)
	}
	return claims, nil
}

func audienceContains(aud any, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return v == clientID
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func fetchRSAKey(ctx context.Context, jwksURL, kid string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("jwks %s", resp.Status)
	}
	var set jwks
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, err
	}
	var chosen *jwk
	for i := range set.Keys {
		k := &set.Keys[i]
		if k.Kty != "RSA" {
			continue
		}
		if kid != "" && k.Kid != kid {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		chosen = k
		break
	}
	if chosen == nil {
		return nil, fmt.Errorf("no matching jwk")
	}
	nb, err := base64.RawURLEncoding.DecodeString(chosen.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(chosen.E)
	if err != nil {
		return nil, err
	}
	var e int
	for _, b := range eb {
		e = e<<8 + int(b)
	}
	if e == 0 {
		e = 65537
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, nil
}

func (c Config) UserInfo(ctx context.Context, p Provider, accessToken string) (UserInfo, error) {
	if p.UserURL == "" {
		return UserInfo{}, fmt.Errorf("no userinfo endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.UserURL, nil)
	if err != nil {
		return UserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return UserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return UserInfo{}, fmt.Errorf("userinfo: %s %s", resp.Status, string(b))
	}
	var u UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return UserInfo{}, err
	}
	if u.Email == "" {
		u.Email = u.Sub + "@oidc.local"
	}
	if u.Name == "" {
		u.Name = u.Email
	}
	return u, nil
}

// RequireVerified returns error if email_verified is explicitly false.
func RequireVerified(verified *bool) error {
	if verified != nil && !*verified {
		return fmt.Errorf("email not verified")
	}
	return nil
}
