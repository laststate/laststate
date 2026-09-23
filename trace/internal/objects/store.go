package objects

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store is FS by default; optional S3-compatible (MinIO/R2) when Endpoint is set.
type Store struct {
	Root string

	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string

	mu     sync.Mutex
	client *http.Client
	cache  string
}

func (s *Store) s3Enabled() bool {
	return strings.TrimSpace(s.Endpoint) != "" && s.Bucket != ""
}

func (s *Store) Ensure() error {
	if s.Root == "" {
		return fmt.Errorf("object dir empty")
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	s.cache = filepath.Join(s.Root, ".cache")
	return os.MkdirAll(s.cache, 0o755)
}

// Put writes raw bytes durably. Local FS: write unique temp → fsync → rename → fsync dir.
// Existing objects are re-read and SHA-256/size verified before short-circuit.
func (s *Store) Put(raw []byte) (key, hash string, err error) {
	sum := sha256.Sum256(raw)
	hash = hex.EncodeToString(sum[:])
	key = filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], hash))
	if s.s3Enabled() {
		if existing, gerr := s.s3Get(key); gerr == nil {
			if int64(len(existing)) == int64(len(raw)) {
				es := sha256.Sum256(existing)
				if hex.EncodeToString(es[:]) == hash {
					return key, hash, nil
				}
			}
			return "", "", fmt.Errorf("object %s exists with different content", key)
		}
		return key, hash, s.s3Put(key, raw)
	}
	full := filepath.Join(s.Root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", "", err
	}
	if st, err := os.Stat(full); err == nil {
		if st.Size() != int64(len(raw)) {
			return "", "", fmt.Errorf("object %s size mismatch", key)
		}
		existing, rerr := os.ReadFile(full)
		if rerr != nil {
			return "", "", rerr
		}
		es := sha256.Sum256(existing)
		if hex.EncodeToString(es[:]) != hash {
			return "", "", fmt.Errorf("object %s hash mismatch", key)
		}
		return key, hash, nil
	}
	// unique temp name avoids races between concurrent writers of the same hash
	rnd := make([]byte, 8)
	_, _ = crand.Read(rnd)
	tmp := full + ".tmp." + hex.EncodeToString(sum[:4]) + "." + hex.EncodeToString(rnd)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return "", "", err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		// lost race to another writer — verify winner
		if existing, rerr := os.ReadFile(full); rerr == nil {
			es := sha256.Sum256(existing)
			if hex.EncodeToString(es[:]) == hash {
				return key, hash, nil
			}
		}
		return "", "", err
	}
	if dir, err := os.Open(filepath.Dir(full)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return key, hash, nil
}

func (s *Store) Get(key string) ([]byte, error) {
	if s.s3Enabled() {
		return s.s3Get(key)
	}
	return os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(key)))
}

func (s *Store) Delete(key string) error {
	if s.s3Enabled() {
		return s.s3Delete(key)
	}
	return os.Remove(filepath.Join(s.Root, filepath.FromSlash(key)))
}

// ListKeys returns known object keys (FS walk or empty for S3 without list API).
func (s *Store) ListKeys() ([]string, error) {
	if s.s3Enabled() {
		return s.s3List()
	}
	var keys []string
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+".cache"+string(filepath.Separator)) {
			return nil
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		parts := strings.Split(key, "/")
		if len(parts) == 3 && len(parts[2]) >= 32 {
			keys = append(keys, key)
		}
		return nil
	})
	return keys, err
}

func (s *Store) s3List() ([]string, error) {
	// ListObjectsV2 minimal via S3 API
	base := strings.TrimRight(s.Endpoint, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.Bucket
	q := u.Query()
	q.Set("list-type", "2")
	q.Set("max-keys", "1000")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := s.sign(req, nil); err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("s3 list %s: %s", resp.Status, string(body))
	}
	// crude XML key extract
	var keys []string
	sbody := string(body)
	for {
		i := strings.Index(sbody, "<Key>")
		if i < 0 {
			break
		}
		sbody = sbody[i+5:]
		j := strings.Index(sbody, "</Key>")
		if j < 0 {
			break
		}
		keys = append(keys, sbody[:j])
		sbody = sbody[j+6:]
	}
	return keys, nil
}

func (s *Store) Path(key string) string {
	if !s.s3Enabled() {
		return filepath.Join(s.Root, filepath.FromSlash(key))
	}
	cache := s.cache
	if cache == "" {
		cache = filepath.Join(s.Root, ".cache")
	}
	local := filepath.Join(cache, filepath.FromSlash(key))
	if _, err := os.Stat(local); err == nil {
		return local
	}
	raw, err := s.s3Get(key)
	if err != nil {
		return local
	}
	_ = os.MkdirAll(filepath.Dir(local), 0o755)
	_ = os.WriteFile(local, raw, 0o644)
	return local
}

func (s *Store) httpClient() *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		s.client = &http.Client{Timeout: 60 * time.Second}
	}
	return s.client
}

func (s *Store) objectURL(key string) (string, error) {
	base := strings.TrimRight(s.Endpoint, "/")
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.Bucket + "/" + strings.TrimPrefix(key, "/")
	return u.String(), nil
}

func (s *Store) s3Put(key string, raw []byte) error {
	u, err := s.objectURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(raw))
	if err := s.sign(req, raw); err != nil {
		return err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("s3 put %s: %s", resp.Status, string(b))
	}
	return nil
}

func (s *Store) s3Get(key string) ([]byte, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if err := s.sign(req, nil); err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("s3 get %s: %s", resp.Status, string(b))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

func (s *Store) s3Delete(key string) error {
	u, err := s.objectURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	if err := s.sign(req, nil); err != nil {
		return err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("s3 delete %s: %s", resp.Status, string(b))
	}
	return nil
}

func (s *Store) sign(req *http.Request, body []byte) error {
	if s.AccessKey == "" || s.SecretKey == "" {
		return nil
	}
	return signAWS4(req, body, s.AccessKey, s.SecretKey, s.Region, s.Bucket)
}
