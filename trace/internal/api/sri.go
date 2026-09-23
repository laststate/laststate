package api

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// sriAlgorithms maps a SHA prefix to the Go hashing function and digest size.
var sriAlgorithms = map[string]struct {
	sum  func([]byte) [32]byte // sha256 returns [32]byte
	size int
}{
	"sha256": {sha256.Sum256, 32},
}

// sriSum384 returns a 48-byte SHA-384 digest.
func sriSum384(data []byte) [48]byte { return sha512.Sum384(data) }

// sriSum512 returns a 64-byte SHA-512 digest.
func sriSum512(data []byte) [64]byte { return sha512.Sum512(data) }

// sriSumAlgo returns the appropriate hash function and size for the given algorithm.
func sriSumAlgo(algo string) (func([]byte) []byte, int) {
	switch algo {
	case "sha256":
		s := sha256.Sum256(nil)
		return func(data []byte) []byte { s2 := sha256.Sum256(data); return s2[:] }, len(s)
	case "sha384":
		return func(data []byte) []byte { s := sha512.Sum384(data); return s[:] }, 48
	case "sha512":
		return func(data []byte) []byte { s := sha512.Sum512(data); return s[:] }, 64
	}
	return func([]byte) []byte { return nil }, 0
}

// sriFromHash returns an SRI integrity attribute value per RFC 9206.
// The format is: <algorithm>-<base64-digest>
func sriFromHash(algo string, hash []byte) string {
	return fmt.Sprintf("%s-%s", algo, base64.StdEncoding.EncodeToString(hash))
}

// sriMiddleware wraps an http.FileSystem so that HTML responses have SRI hashes
// injected into their <script> and <link> tags. Only responses with
// Content-Type starting with "text/html" are modified.
//
// The middleware computes a SHA-256 hash of the **referenced asset file** and
// injects it as the `integrity` attribute on every <script> and
// <link rel="stylesheet"> tag in the HTML. The `crossorigin` attribute is
// added when absent.
func sriMiddleware(fs http.FileSystem) http.Handler {
	cache := &sriCache{fs: fs, cache: make(map[string][]byte)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fast path: skip if the response is already HTML — we'll handle it
		// via the wrapped writer.
		wrw := &sriResponseWriter{ResponseWriter: w}
		cache.serveFile(wrw, r)
	})
}

// sriResponseWriter wraps http.ResponseWriter to capture the content type
// and inject SRI attributes into HTML bodies before they are written.
type sriResponseWriter struct {
	http.ResponseWriter
	status      int
	body        []byte
	headerSent  bool
	contentType string
}

func (w *sriResponseWriter) WriteHeader(code int) {
	w.status = code
	w.headerSent = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *sriResponseWriter) Write(b []byte) (int, error) {
	if !w.headerSent {
		w.WriteHeader(http.StatusOK)
	}
	ct := w.ResponseWriter.Header().Get("Content-Type")
	w.contentType = ct
	w.body = append(w.body, b...)
	return len(b), nil
}

// mimeByPath returns a MIME type from the file extension, falling back to
// http.DetectContentType only when the extension is unknown. This avoids
// the pitfall where DetectContentType sniffs CSS/JS as "text/plain".
func mimeByPath(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	// Explicit overrides for web assets — avoids relying on host mime.types
	// and fixes Go's builtin mapping for .map → text/plain.
	switch ext {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ico":
		return "image/x-icon"
	}
	if ext != "" {
		if ct := mime.TypeByExtension(ext); ct != "" {
			return ct
		}
	}
	// Last resort: sniff content. If it still looks like text/plain but
	// extension was known, prefer the extension-based type above.
	ct := http.DetectContentType(data)
	if strings.HasPrefix(ct, "text/plain") && ext != "" {
		// Unknown extension that looks like text — keep sniffed type
		return ct
	}
	return ct
}

func (c *sriCache) serveFile(w *sriResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Look up cached hash.
	c.mu.RLock()
	data, ok := c.cache[path]
	c.mu.RUnlock()

	if !ok {
		f, err := c.fs.Open(path)
		if err != nil {
			http.NotFound(w.ResponseWriter, r)
			return
		}
		raw, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			http.Error(w.ResponseWriter, "internal error", http.StatusInternalServerError)
			return
		}
		c.mu.Lock()
		c.cache[path] = raw
		c.mu.Unlock()
		data = raw
	}

	ct := mimeByPath(path, data)
	if !strings.HasPrefix(ct, "text/html") {
		// Non-HTML: pass through unchanged with correct MIME type.
		w.ResponseWriter.Header().Set("Content-Type", ct)
		w.ResponseWriter.Write(data)
		return
	}

	// HTML: inject SRI into script and link tags, then serve.
	injected := c.injectSRI(string(data))
	w.ResponseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.ResponseWriter.Write([]byte(injected))
}

// sriCache stores file contents for repeated requests.
type sriCache struct {
	fs    http.FileSystem
	mu    sync.RWMutex
	cache map[string][]byte
}

// readAsset returns the raw bytes of an asset from the filesystem cache,
// loading it on first access.
func (c *sriCache) readAsset(path string) ([]byte, error) {
	c.mu.RLock()
	data, ok := c.cache[path]
	c.mu.RUnlock()
	if ok {
		return data, nil
	}

	f, err := c.fs.Open(path)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[path] = raw
	c.mu.Unlock()
	return raw, nil
}

// injectSRI adds integrity and crossorigin attributes to <script> and
// <link rel="stylesheet"> tags in an HTML document. Tags that already have
// an integrity attribute are left untouched.
//
// Unlike the previous implementation, this hashes the **referenced file
// content**, not the HTML tag string itself. The browser validates the
// integrity attribute against the downloaded asset body — so we must do the
// same.
func (c *sriCache) injectSRI(html string) string {
	html = c.injectScriptIntegrity(html)
	html = c.injectLinkIntegrity(html)
	return html
}

var reSrc = regexp.MustCompile(`(?i)\bsrc=["']([^"']+)["']`)

// injectScriptIntegrity adds integrity and crossorigin attributes to <script>
// tags missing them.
func (c *sriCache) injectScriptIntegrity(html string) string {
	re := regexp.MustCompile(`(?i)(<script\b)([^>]*)>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		if strings.Contains(match, "integrity=") || strings.Contains(match, "crossorigin=") {
			return match
		}
		m := reSrc.FindStringSubmatch(match)
		if m == nil {
			return match // inline script, skip
		}
		assetPath := m[1]
		data, err := c.readAsset(assetPath)
		if err != nil {
			return match // can't read asset, skip injection
		}
		sum := sha256.Sum256(data)
		sri := sriFromHash("sha256", sum[:])
		return strings.TrimSuffix(match, ">") +
			fmt.Sprintf(` integrity="%s" crossorigin="anonymous">`, sri)
	})
}

var reHref = regexp.MustCompile(`(?i)\bhref=["']([^"']+)["']`)

// injectLinkIntegrity adds integrity and crossorigin attributes to <link
// rel="stylesheet"> tags missing them.
func (c *sriCache) injectLinkIntegrity(html string) string {
	re := regexp.MustCompile(`(?i)(<link\b[^>]*\brel=["']stylesheet["'][^>]*)>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		if strings.Contains(match, "integrity=") || strings.Contains(match, "crossorigin=") {
			return match
		}
		m := reHref.FindStringSubmatch(match)
		if m == nil {
			return match // no href, skip
		}
		assetPath := m[1]
		data, err := c.readAsset(assetPath)
		if err != nil {
			return match // can't read asset, skip injection
		}
		sum := sha256.Sum256(data)
		sri := sriFromHash("sha256", sum[:])
		return strings.TrimSuffix(match, ">") +
			fmt.Sprintf(` integrity="%s" crossorigin="anonymous"/>`, sri)
	})
}
