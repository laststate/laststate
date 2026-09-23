// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package match

import (
	"testing"

	"github.com/laststate/relay/internal/artifact"
	"github.com/laststate/relay/internal/lep"
)

func TestResolvePrefersExactBuildID(t *testing.T) {
	catalog := []artifact.Artifact{
		{SHA256: "aaa", BuildID: "deadbeef", HasDWARF: true},
		{SHA256: "bbb", BuildID: "cafebabe", HasDWARF: false},
	}
	result := Resolve(Identity{BuildID: "deadbeef"}, catalog)
	if result.Artifact == nil || result.Artifact.SHA256 != "aaa" {
		t.Fatalf("%#v", result)
	}
	if result.Ambiguous || result.Confidence < 0.9 {
		t.Fatalf("%#v", result)
	}
}

func TestResolveAmbiguityWarning(t *testing.T) {
	catalog := []artifact.Artifact{
		{SHA256: "aaa", BuildID: "same", HasDWARF: false},
		{SHA256: "bbb", BuildID: "same", HasDWARF: true},
	}
	result := Resolve(Identity{BuildID: "same"}, catalog)
	if !result.Ambiguous || result.Artifact == nil || result.Artifact.SHA256 != "bbb" {
		t.Fatalf("%#v", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings")
	}
}

func TestExtractIdentityFromTLV(t *testing.T) {
	fields := []lep.TLV{
		{Type: TLVBuildID, Value: []byte("abcd1234")},
		{Type: TLVProjectID, Value: []byte("proj")},
	}
	raw, err := lep.EncodeTLVs(lep.Version1, 1, 1, 1, 1, fields)
	if err != nil {
		t.Fatal(err)
	}
	id, err := ExtractIdentity(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id.BuildID != "abcd1234" || id.ProjectID != "proj" {
		t.Fatalf("%#v", id)
	}
}
