// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package symbolicate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Cache is a disk-backed address → frame map keyed by artifact SHA-256.
type Cache struct {
	Dir string
	mu  sync.Mutex
}

func (cache *Cache) path(artifactSHA string, address uint64) string {
	return filepath.Join(cache.Dir, artifactSHA, fmt.Sprintf("%016x.json", address))
}

func (cache *Cache) Get(artifactSHA string, address uint64) (Frame, bool) {
	if cache == nil || cache.Dir == "" || artifactSHA == "" {
		return Frame{}, false
	}
	data, err := os.ReadFile(cache.path(artifactSHA, address))
	if err != nil {
		return Frame{}, false
	}
	var frame Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		return Frame{}, false
	}
	return frame, true
}

func (cache *Cache) Put(artifactSHA string, frame Frame) error {
	if cache == nil || cache.Dir == "" || artifactSHA == "" {
		return nil
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	dir := filepath.Join(cache.Dir, artifactSHA)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	path := cache.path(artifactSHA, frame.Address)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
