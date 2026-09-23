// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package match finds firmware artifacts from build-id and related TLVs.
package match

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/laststate/relay/internal/artifact"
	"github.com/laststate/relay/internal/lep"
)

// Identity-related TLV type IDs (see spec/lep-tlv-registry.md).
const (
	TLVBuildID      uint16 = 0x0010
	TLVProjectID    uint16 = 0x0011
	TLVReleaseID    uint16 = 0x0012
	TLVFirmwareHash uint16 = 0x0013
)

type Identity struct {
	BuildID      string
	ProjectID    string
	ReleaseID    string
	FirmwareHash string
}

type Result struct {
	Artifact     *artifact.Artifact
	Confidence   float64
	Ambiguous    bool
	Alternatives []artifact.Artifact
	Warnings     []string
	Identity     Identity
}

// ExtractIdentity reads build/project/release/firmware TLVs from an envelope.
// When ring is non-nil, authenticated/encrypted/compressed payloads are decoded first.
func ExtractIdentity(raw []byte, ring lep.Keyring) (Identity, error) {
	fields, err := lep.DecodeTLVs(raw, ring)
	if err != nil {
		fields, err = lep.TLVs(raw)
		if err != nil {
			return Identity{}, err
		}
	}
	var id Identity
	for _, field := range fields {
		switch field.Type {
		case TLVBuildID:
			id.BuildID = strings.ToLower(hex.EncodeToString(field.Value))
			if isPrintable(field.Value) {
				id.BuildID = strings.ToLower(strings.TrimSpace(string(field.Value)))
			}
		case TLVProjectID:
			id.ProjectID = string(field.Value)
		case TLVReleaseID:
			id.ReleaseID = string(field.Value)
		case TLVFirmwareHash:
			id.FirmwareHash = strings.ToLower(hex.EncodeToString(field.Value))
		}
	}
	return id, nil
}

// Resolve picks the best artifact for an event identity from a catalog.
func Resolve(identity Identity, catalog []artifact.Artifact) Result {
	result := Result{Identity: identity, Confidence: 0}
	if identity.BuildID == "" && identity.FirmwareHash == "" {
		result.Warnings = append(result.Warnings, "no build id or firmware hash in event; cannot auto-match")
		return result
	}

	var hits []artifact.Artifact
	for _, item := range catalog {
		if identity.BuildID != "" && strings.EqualFold(item.BuildID, identity.BuildID) {
			hits = append(hits, item)
			continue
		}
		if identity.FirmwareHash != "" && strings.EqualFold(item.SHA256, identity.FirmwareHash) {
			hits = append(hits, item)
		}
	}
	if len(hits) == 0 {
		result.Warnings = append(result.Warnings, "no artifact matched build id or firmware hash")
		return result
	}
	if len(hits) == 1 {
		item := hits[0]
		result.Artifact = &item
		result.Confidence = 0.95
		if identity.BuildID != "" && strings.EqualFold(item.BuildID, identity.BuildID) {
			result.Confidence = 0.99
		}
		return result
	}
	// Prefer DWARF-bearing artifacts when ambiguous.
	var withDWARF []artifact.Artifact
	for _, item := range hits {
		if item.HasDWARF {
			withDWARF = append(withDWARF, item)
		}
	}
	result.Ambiguous = true
	result.Alternatives = hits
	result.Warnings = append(result.Warnings, fmt.Sprintf("ambiguous match: %d artifacts share the same identity", len(hits)))
	chosen := hits[0]
	if len(withDWARF) == 1 {
		chosen = withDWARF[0]
		result.Confidence = 0.7
		result.Warnings = append(result.Warnings, "selected the only DWARF-bearing candidate among ambiguous matches")
	} else {
		result.Confidence = 0.4
		result.Warnings = append(result.Warnings, "selected first catalog hit; verify release identity manually")
	}
	result.Artifact = &chosen
	return result
}

// ResolveFromEvent validates raw LEP, extracts identity, and matches catalog.
func ResolveFromEvent(raw []byte, catalog []artifact.Artifact, ring lep.Keyring) (Result, error) {
	identity, err := ExtractIdentity(raw, ring)
	if err != nil {
		return Result{}, err
	}
	return Resolve(identity, catalog), nil
}

func isPrintable(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for _, b := range value {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}
