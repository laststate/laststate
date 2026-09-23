// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestSignVerifyManifest(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Version: Version,
		RelayID: "relay-1",
		Events: []ManifestEvent{
			{ID: "evt_b", Path: "events/evt_b.lep", SHA256: "bb", Size: 2},
			{ID: "evt_a", Path: "events/evt_a.lep", SHA256: "aa", Size: 1},
		},
	}
	sig, err := SignManifest(manifest, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyManifest(manifest, sig, pub); err != nil {
		t.Fatal(err)
	}
	manifest.Events[0].SHA256 = "tampered"
	if err := VerifyManifest(manifest, sig, pub); err == nil {
		t.Fatal("expected failure after tamper")
	}
}
