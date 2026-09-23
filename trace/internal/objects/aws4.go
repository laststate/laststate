package objects

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func signAWS4(req *http.Request, body []byte, accessKey, secretKey, region, bucket string) error {
	if region == "" {
		region = "us-east-1"
	}
	if body == nil {
		body = []byte{}
	}
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(body)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Host", req.URL.Host)

	canonicalHeaders, signedHeaders := canonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		normalizePath(req.URL.Path),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := aws4SigningKey(secretKey, dateStamp, region, "s3")
	sig := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credentialScope, signedHeaders, sig)
	req.Header.Set("Authorization", auth)
	_ = bucket
	return nil
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	// encode path segments
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	out := strings.Join(parts, "/")
	if !strings.HasPrefix(out, "/") {
		out = "/" + out
	}
	return out
}

func canonicalHeaders(req *http.Request) (string, string) {
	type kv struct{ k, v string }
	var list []kv
	for k, vals := range req.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" {
			continue
		}
		list = append(list, kv{lk, strings.TrimSpace(strings.Join(vals, ","))})
	}
	// host always
	sort.Slice(list, func(i, j int) bool { return list[i].k < list[j].k })
	var b strings.Builder
	var names []string
	for _, h := range list {
		b.WriteString(h.k)
		b.WriteByte(':')
		b.WriteString(h.v)
		b.WriteByte('\n')
		names = append(names, h.k)
	}
	return b.String(), strings.Join(names, ";")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func aws4SigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}
