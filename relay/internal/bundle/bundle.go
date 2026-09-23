// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package bundle exports and imports .lsbundle ZIP archives.
package bundle

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/laststate/relay/internal/ingest"
	"github.com/laststate/relay/internal/store"
)

const Version = 1
const maxImportedEvent = 4 << 20
const maxManifest = 2 << 20

type Manifest struct {
	Version    int                `json:"version"`
	CreatedAt  time.Time          `json:"created_at"`
	RelayID    string             `json:"relay_id,omitempty"`
	Events     []ManifestEvent    `json:"events"`
	Artifacts  []ManifestArtifact `json:"artifacts,omitempty"`
	Signatures []Signature        `json:"signatures,omitempty"`
}

type ManifestEvent struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	SourceID string `json:"source_id,omitempty"`
}

type ManifestArtifact struct {
	SHA256 string `json:"sha256"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
}

type ImportResult struct {
	Imported   int `json:"imported"`
	Duplicates int `json:"duplicates"`
}

// ExportOptions configures optional bundle signing at export time.
type ExportOptions struct {
	// PrivateKey is a 64-byte ed25519 private key (or 32-byte seed expanded by NewKeyFromSeed).
	PrivateKey ed25519.PrivateKey
	KeyID      string
}

// ImportOptions configures signature verification during import.
type ImportOptions struct {
	// PublicKey verifies manifest signatures when set.
	PublicKey ed25519.PublicKey
	// RequireSignature rejects bundles that lack a verifiable signature.
	RequireSignature bool
}

// Export writes events into a .lsbundle ZIP archive.
func Export(ctx context.Context, relay *store.Store, output string, limit int) (Manifest, error) {
	return ExportWithOptions(ctx, relay, output, limit, ExportOptions{})
}

// ExportWithOptions is Export plus optional Ed25519 signing of the canonical manifest.
func ExportWithOptions(ctx context.Context, relay *store.Store, output string, limit int, opts ExportOptions) (Manifest, error) {
	events, err := relay.List(ctx, limit, 0)
	if err != nil {
		return Manifest{}, err
	}
	status, _ := relay.Status(ctx)
	manifest := Manifest{Version: Version, CreatedAt: time.Now().UTC(), RelayID: status.RelayID}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".pending-bundle-*.zip")
	if err != nil {
		return Manifest{}, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	archive := zip.NewWriter(temporary)
	for _, event := range events {
		raw, err := relay.RawEvent(ctx, event.ID)
		if err != nil {
			archive.Close()
			temporary.Close()
			return Manifest{}, err
		}
		path := "events/" + event.ID + ".lep"
		writer, err := archive.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Store})
		if err != nil {
			archive.Close()
			temporary.Close()
			return Manifest{}, err
		}
		if _, err := writer.Write(raw); err != nil {
			archive.Close()
			temporary.Close()
			return Manifest{}, err
		}
		hash := sha256.Sum256(raw)
		manifest.Events = append(manifest.Events, ManifestEvent{ID: event.ID, Path: path, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(raw)), SourceID: event.SourceID})
	}
	sort.Slice(manifest.Events, func(i, j int) bool { return manifest.Events[i].ID < manifest.Events[j].ID })
	if len(opts.PrivateKey) > 0 {
		keyID := opts.KeyID
		if keyID == "" {
			keyID = "default"
		}
		sig, err := SignManifest(manifest, keyID, opts.PrivateKey)
		if err != nil {
			archive.Close()
			temporary.Close()
			return Manifest{}, err
		}
		manifest.Signatures = append(manifest.Signatures, sig)
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		archive.Close()
		temporary.Close()
		return Manifest{}, err
	}
	manifestWriter, err := archive.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Deflate})
	if err != nil {
		archive.Close()
		temporary.Close()
		return Manifest{}, err
	}
	if _, err := manifestWriter.Write(append(manifestBytes, '\n')); err != nil {
		archive.Close()
		temporary.Close()
		return Manifest{}, err
	}
	if err := archive.Close(); err != nil {
		temporary.Close()
		return Manifest{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Manifest{}, err
	}
	if err := temporary.Close(); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(name, output); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Import loads events from a .lsbundle into the normal durable ingest path.
func Import(ctx context.Context, service ingest.Service, path, sourceID string) (ImportResult, error) {
	return ImportWithOptions(ctx, service, path, sourceID, ImportOptions{})
}

// ImportWithOptions is Import plus optional signature enforcement.
func ImportWithOptions(ctx context.Context, service ingest.Service, path, sourceID string, opts ImportOptions) (ImportResult, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer archive.Close()
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		clean := filepath.ToSlash(filepath.Clean(file.Name))
		if clean != file.Name || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return ImportResult{}, fmt.Errorf("unsafe bundle path %q", file.Name)
		}
		files[file.Name] = file
	}
	manifestFile := files["manifest.json"]
	if manifestFile == nil {
		return ImportResult{}, fmt.Errorf("bundle has no manifest.json")
	}
	manifestReader, err := manifestFile.Open()
	if err != nil {
		return ImportResult{}, err
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(manifestReader, maxManifest+1))
	manifestReader.Close()
	if err != nil {
		return ImportResult{}, err
	}
	if len(manifestBytes) > maxManifest {
		return ImportResult{}, fmt.Errorf("bundle manifest exceeds %d bytes", maxManifest)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ImportResult{}, err
	}
	if manifest.Version != Version {
		return ImportResult{}, fmt.Errorf("unsupported bundle version %d", manifest.Version)
	}
	if err := verifyImportSignatures(manifest, opts); err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	for _, event := range manifest.Events {
		file := files[event.Path]
		if file == nil {
			return result, fmt.Errorf("bundle event %s is missing", event.Path)
		}
		if file.UncompressedSize64 > maxImportedEvent || int64(file.UncompressedSize64) != event.Size {
			return result, fmt.Errorf("bundle event %s has invalid size", event.ID)
		}
		reader, err := file.Open()
		if err != nil {
			return result, err
		}
		raw, err := io.ReadAll(io.LimitReader(reader, maxImportedEvent+1))
		reader.Close()
		if err != nil {
			return result, err
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != event.SHA256 {
			return result, fmt.Errorf("bundle event %s checksum mismatch", event.ID)
		}
		accepted, err := service.Accept(ctx, sourceID, raw)
		if err != nil {
			return result, fmt.Errorf("import event %s: %w", event.ID, err)
		}
		if accepted.Duplicate {
			result.Duplicates++
		} else {
			result.Imported++
		}
	}
	return result, nil
}

func verifyImportSignatures(manifest Manifest, opts ImportOptions) error {
	if len(opts.PublicKey) == 0 && !opts.RequireSignature {
		return nil
	}
	if len(manifest.Signatures) == 0 {
		if opts.RequireSignature {
			return fmt.Errorf("bundle signature required but manifest has none")
		}
		return nil
	}
	if len(opts.PublicKey) == 0 {
		return fmt.Errorf("bundle is signed but no public key was provided for verification")
	}
	var last error
	for _, signature := range manifest.Signatures {
		if err := VerifyManifest(manifest, signature, opts.PublicKey); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last == nil {
		last = fmt.Errorf("no valid bundle signature")
	}
	return last
}
