// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package bundle

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
)

// Signature is a detached Ed25519 signature over the canonical manifest digest.
type Signature struct {
	Alg       string `json:"alg"` // ed25519
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"` // base64
}

// CanonicalManifestJSON produces a stable JSON encoding for signing.
// Events are sorted by ID; only core integrity fields are included.
func CanonicalManifestJSON(manifest Manifest) ([]byte, error) {
	type canonEvent struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	type canonArtifact struct {
		SHA256 string `json:"sha256"`
		Path   string `json:"path"`
		Size   int64  `json:"size"`
	}
	type canon struct {
		Version   int             `json:"version"`
		RelayID   string          `json:"relay_id,omitempty"`
		Events    []canonEvent    `json:"events"`
		Artifacts []canonArtifact `json:"artifacts,omitempty"`
	}
	payload := canon{Version: manifest.Version, RelayID: manifest.RelayID}
	events := append([]ManifestEvent(nil), manifest.Events...)
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	for _, event := range events {
		payload.Events = append(payload.Events, canonEvent{ID: event.ID, Path: event.Path, SHA256: event.SHA256, Size: event.Size})
	}
	artifacts := append([]ManifestArtifact(nil), manifest.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].SHA256 < artifacts[j].SHA256 })
	for _, item := range artifacts {
		payload.Artifacts = append(payload.Artifacts, canonArtifact(item))
	}
	return json.Marshal(payload)
}

// ManifestDigest returns SHA-256 of the canonical manifest JSON.
func ManifestDigest(manifest Manifest) ([32]byte, error) {
	raw, err := CanonicalManifestJSON(manifest)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// SignManifest creates an Ed25519 signature entry for the manifest.
func SignManifest(manifest Manifest, keyID string, private ed25519.PrivateKey) (Signature, error) {
	digest, err := ManifestDigest(manifest)
	if err != nil {
		return Signature{}, err
	}
	sig := ed25519.Sign(private, digest[:])
	return Signature{
		Alg:       "ed25519",
		KeyID:     keyID,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// VerifyManifest checks one signature against a public key.
func VerifyManifest(manifest Manifest, signature Signature, public ed25519.PublicKey) error {
	if signature.Alg != "ed25519" {
		return fmt.Errorf("unsupported signature algorithm %q", signature.Alg)
	}
	raw, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	digest, err := ManifestDigest(manifest)
	if err != nil {
		return err
	}
	if !ed25519.Verify(public, digest[:], raw) {
		return fmt.Errorf("manifest signature verification failed")
	}
	return nil
}
