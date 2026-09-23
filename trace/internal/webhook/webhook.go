// Package webhook delivers signed outbound notifications with SSRF protection.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Delivery struct {
	StatusCode int
	Body       string
	Success    bool
}

// ValidateURL rejects private/link-local/metadata targets (SSRF).
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("url scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || lower == "metadata.google.internal" {
		return fmt.Errorf("blocked host")
	}
	// Block common cloud metadata hostnames
	blocked := []string{
		"169.254.169.254", "metadata", "metadata.google.internal",
		"instance-data", "kubernetes.default", "kubernetes.default.svc",
	}
	for _, b := range blocked {
		if lower == b || strings.HasPrefix(lower, b+".") {
			return fmt.Errorf("blocked host")
		}
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// allow if DNS fails at validate time? safer to reject
		return fmt.Errorf("cannot resolve host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no addresses for host")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("blocked address %s", ip)
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	// IPv4-mapped and CGNAT
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 link-local (covered), 100.64/10 CGNAT
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}

// dialContext blocks connections to private IPs even after redirect/DNS rebinding.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	d := net.Dialer{Timeout: 5 * time.Second}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			last = fmt.Errorf("blocked address %s", ipa.IP)
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no safe addresses")
	}
	return nil, last
}

// Send posts JSON with HMAC headers. Timestamp + body signed to prevent replay.
func Send(ctx context.Context, targetURL, secret string, payload any) (Delivery, error) {
	if err := ValidateURL(targetURL); err != nil {
		return Delivery{}, fmt.Errorf("ssrf: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Delivery{}, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := sign(secret, ts, raw)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return Delivery{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Last-State-Timestamp", ts)
	req.Header.Set("X-Last-State-Signature", "sha256="+sig)
	req.Header.Set("User-Agent", "laststate-trace/0.3")

	transport := &http.Transport{
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		// Disable redirects to prevent SSRF via open redirect
		// (CheckRedirect set on client)
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return fmt.Errorf("too many redirects")
			}
			return ValidateURL(req.URL.String())
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return Delivery{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	return Delivery{StatusCode: resp.StatusCode, Body: string(b), Success: ok}, nil
}

func sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify is for receivers/tests.
func Verify(secret, ts, sigHeader string, body []byte) error {
	want := "sha256=" + sign(secret, ts, body)
	if !hmac.Equal([]byte(want), []byte(sigHeader)) {
		return fmt.Errorf("bad signature")
	}
	return nil
}
