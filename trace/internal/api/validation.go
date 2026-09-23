package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ValidateInput validates common input patterns to prevent injection attacks
func ValidateInput(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Block common injection patterns
	blocked := []string{
		"<'",
		"</",
		"javascript:",
		"data:",
		"vbscript:",
		"onerror=",
		"onload=",
		"onclick=",
		"onmouseover=",
		"<script",
		"<iframe",
		"<object",
		"<embed",
		"<applet",
		"eval(",
		"exec(",
		"system(",
		"shell_exec(",
		"passthru(",
		"popen(",
		"proc_open(",
		"deserialization",
		"__construct",
		"__destruct",
		"__wakeup",
	}
	sLower := strings.ToLower(s)
	for _, pattern := range blocked {
		if strings.Contains(sLower, pattern) {
			return false
		}
	}
	return true
}

// ValidateEmail validates email format
func ValidateEmail(email string) bool {
	if len(email) == 0 || len(email) > 254 {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	local := parts[0]
	domain := parts[1]
	if len(local) == 0 || len(local) > 64 {
		return false
	}
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	// Basic domain validation
	if !strings.Contains(domain, ".") {
		return false
	}
	return true
}

// ValidateProjectSlug validates project slug format
func ValidateProjectSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 63 {
		return false
	}
	for _, c := range slug {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// ValidateOrganizationSlug validates organization slug format
func ValidateOrganizationSlug(slug string) bool {
	return ValidateProjectSlug(slug)
}

// ValidateUUID validates UUID format
func ValidateUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ValidateToken validates token format
func ValidateToken(token string) bool {
	if len(token) < 8 || len(token) > 2048 {
		return false
	}
	// Tokens should be alphanumeric with possible dashes and underscores
	for _, c := range token {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ValidateURL validates URL format and blocks private/internal networks
func ValidateURL(u string) bool {
	if len(u) == 0 || len(u) > 2048 {
		return false
	}
	uLower := strings.ToLower(u)
	// Block dangerous schemes
	blockedSchemes := []string{"file://", "gopher://", "ftp://", "expect://", "php://", "data://", "javascript:"}
	for _, s := range blockedSchemes {
		if strings.HasPrefix(uLower, s) {
			return false
		}
	}
	// Parse URL and validate host against private networks
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	// Check hostname against blocklist (handles userinfo bypass like http://example.com@10.0.0.1/)
	hLower := strings.ToLower(host)
	if hLower == "localhost" || hLower == "0.0.0.0" || hLower == "::1" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return false
		}
	}
	// Also block private prefixes on hostname string for non-IP hosts that start with private pattern
	privatePrefixes := []string{"10.", "192.168.", "172.16.", "172.17.", "172.18.", "172.19.", "172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.", "172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31."}
	for _, p := range privatePrefixes {
		if strings.HasPrefix(hLower, p) {
			return false
		}
	}
	return true
}

// IsInternalIP checks if an IP address is internal/private
func IsInternalIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast()
}

// ValidateSeverity validates event severity level
func ValidateSeverity(severity string) bool {
	valid := map[string]bool{
		"fatal":   true,
		"error":   true,
		"warning": true,
		"info":    true,
		"debug":   true,
	}
	return valid[strings.ToLower(severity)]
}

// ValidateStatus validates issue status
func ValidateStatus(status string) bool {
	valid := map[string]bool{
		"open":          true,
		"investigating": true,
		"resolved":      true,
		"ignored":       true,
		"archived":      true,
	}
	return valid[strings.ToLower(status)]
}

// ValidatePipeline validates event pipeline type
func ValidatePipeline(pipeline string) bool {
	valid := map[string]bool{
		"issue":  true,
		"health": true,
		"log":    true,
		"metric": true,
		"boot":   true,
	}
	return valid[strings.ToLower(pipeline)]
}

// ValidateArchitecture validates architecture code
func ValidateArchitecture(arch int16) bool {
	// Valid architecture codes from LEP spec
	valid := map[int16]bool{
		0:  true, // unknown
		1:  true, // x86
		2:  true, // x86_64
		3:  true, // ARM
		4:  true, // ARM64
		5:  true, // RISC-V
		6:  true, // MIPS
		7:  true, // PowerPC
		8:  true, // SPARC
		9:  true, // Alpha
		10: true, // IA-64
		11: true, // S390
		12: true, // Xtensa
		13: true, // LoongArch
	}
	return valid[arch]
}

// ValidateLEPType validates LEP event type
func ValidateLEPType(typ int16) bool {
	valid := map[int16]bool{
		1: true, // health
		2: true, // error
		3: true, // crash
		4: true, // coredump
		5: true, // log
		6: true, // message
		7: true, // peripheral
		8: true, // reset
	}
	return valid[typ]
}

// SanitizeString sanitizes a string for safe output
func SanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	// Remove control characters except newline and tab
	var result strings.Builder
	for _, r := range s {
		if (r >= 32 && r <= 126) || r == '\n' || r == '\t' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// ValidateJSONSize validates JSON payload size
func ValidateJSONSize(body []byte, maxBytes int64) bool {
	if int64(len(body)) > maxBytes {
		return false
	}
	return true
}

// ValidateHeader validates HTTP header value
func ValidateHeader(value string) bool {
	if len(value) == 0 || len(value) > 1024 {
		return false
	}
	// Headers should only contain printable ASCII
	for _, c := range value {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

// ValidateContentType validates Content-Type header
func ValidateContentType(ct string) bool {
	validTypes := map[string]bool{
		"application/json":                   true,
		"application/octet-stream":           true,
		"application/vnd.laststate.batch.v1": true,
		"text/plain":                         true,
		"multipart/form-data":                true,
	}
	return validTypes[strings.ToLower(ct)]
}

// ValidateMethod validates HTTP method
func ValidateMethod(method string) bool {
	validMethods := map[string]bool{
		"GET":     true,
		"POST":    true,
		"PUT":     true,
		"PATCH":   true,
		"DELETE":  true,
		"HEAD":    true,
		"OPTIONS": true,
	}
	return validMethods[strings.ToUpper(method)]
}

// ValidatePath validates URL path
func ValidatePath(path string) bool {
	if len(path) == 0 || len(path) > 2048 {
		return false
	}
	// Decode percent-encoding to catch bypasses like %2e%2e, %252e
	decoded := path
	for i := 0; i < 3; i++ {
		prev := decoded
		if d, err := url.PathUnescape(decoded); err == nil {
			decoded = d
		}
		if decoded == prev {
			break
		}
	}
	// Block path traversal on decoded path
	if strings.Contains(decoded, "..") {
		return false
	}
	// Block null bytes (raw and encoded)
	if strings.Contains(path, "\x00") || strings.Contains(decoded, "\x00") {
		return false
	}
	// Also block encoded null
	if strings.Contains(strings.ToLower(path), "%00") {
		return false
	}
	return true
}

// AddSecurityHeaders adds security headers to response
func AddSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("X-Download-Options", "noopen")
	w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
}
