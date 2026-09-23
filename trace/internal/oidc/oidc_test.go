package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laststate/trace/internal/oidc"
)

func TestEnabledAndS256(t *testing.T) {
	c := oidc.Config{}
	if c.Enabled() {
		t.Fatal()
	}
	c = oidc.Config{Issuer: "https://i", ClientID: "c", RedirectURL: "https://r"}
	if !c.Enabled() {
		t.Fatal()
	}
	ch := oidc.S256Challenge("verifier-value")
	if ch == "" || strings.Contains(ch, "=") {
		t.Fatal(ch)
	}
}

func TestAuthCodeURL(t *testing.T) {
	c := oidc.Config{ClientID: "cid", RedirectURL: "https://cb"}
	p := oidc.Provider{AuthURL: "https://idp/auth"}
	u := c.AuthCodeURL(p, "st", "nn", "ch")
	if !strings.Contains(u, "state=st") || !strings.Contains(u, "nonce=nn") || !strings.Contains(u, "code_challenge=ch") {
		t.Fatal(u)
	}
	if !strings.Contains(u, "code_challenge_method=S256") {
		t.Fatal(u)
	}
}

func TestRequireVerified(t *testing.T) {
	f := false
	if err := oidc.RequireVerified(&f); err == nil {
		t.Fatal()
	}
	tr := true
	if err := oidc.RequireVerified(&tr); err != nil {
		t.Fatal(err)
	}
	if err := oidc.RequireVerified(nil); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAndValidateIDToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	kid := "k1"
	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	}
	var jwksURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "http://" + r.Host,
			"authorization_endpoint": "http://" + r.Host + "/auth",
			"token_endpoint":         "http://" + r.Host + "/token",
			"userinfo_endpoint":      "http://" + r.Host + "/userinfo",
			"jwks_uri":               jwksURL,
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	jwksURL = srv.URL + "/jwks"

	ctx := context.Background()
	p, err := oidc.Discover(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if p.AuthURL == "" || p.JWKSURL == "" {
		t.Fatal(p)
	}

	cfg := oidc.Config{Issuer: srv.URL, ClientID: "myclient", RedirectURL: "https://app/cb"}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid})
	now := time.Now().Unix()
	payload, _ := json.Marshal(map[string]any{
		"iss": srv.URL, "sub": "u1", "aud": "myclient",
		"exp": now + 3600, "iat": now, "nonce": "n-once",
		"email": "a@b.c", "email_verified": true,
	})
	h64 := base64.RawURLEncoding.EncodeToString(header)
	p64 := base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(h64 + "." + p64))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	idTok := h64 + "." + p64 + "." + base64.RawURLEncoding.EncodeToString(sig)

	claims, err := cfg.ValidateIDToken(ctx, p, idTok, "n-once")
	if err != nil {
		t.Fatal(err)
	}
	if claims.Email != "a@b.c" {
		t.Fatal(claims)
	}
	if _, err := cfg.ValidateIDToken(ctx, p, idTok, "wrong"); err == nil {
		t.Fatal("nonce")
	}
	cfg.ClientID = "other"
	if _, err := cfg.ValidateIDToken(ctx, p, idTok, "n-once"); err == nil {
		t.Fatal("aud")
	}
}

func TestValidateRejectsMissingToken(t *testing.T) {
	cfg := oidc.Config{ClientID: "c", Issuer: "https://i"}
	if _, err := cfg.ValidateIDToken(context.Background(), oidc.Provider{Issuer: "https://i"}, "", ""); err == nil {
		t.Fatal()
	}
	if _, err := cfg.ValidateIDToken(context.Background(), oidc.Provider{Issuer: "https://i"}, "a.b", ""); err == nil {
		t.Fatal("malformed")
	}
}

func TestExchangeAndUserInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "at", "id_token": "id", "token_type": "bearer",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": "u", "email": "e@x.com", "name": "N"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := oidc.Config{ClientID: "c", ClientSecret: "s", RedirectURL: "https://cb"}
	p := oidc.Provider{TokenURL: srv.URL + "/token", UserURL: srv.URL + "/userinfo"}
	tr, err := cfg.Exchange(context.Background(), p, "code", "ver")
	if err != nil || tr.AccessToken != "at" {
		t.Fatal(err, tr)
	}
	ui, err := cfg.UserInfo(context.Background(), p, "at")
	if err != nil || ui.Email != "e@x.com" {
		t.Fatal(err, ui)
	}
}
