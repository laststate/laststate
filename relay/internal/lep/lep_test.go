// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package lep

import (
	"encoding/hex"
	"testing"
)

func TestValidateLatchGoldenVector(t *testing.T) {
	raw, err := hex.DecodeString("4c5354500102000007000000090000000600000074ddc48901000200aabbea84ccd8")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Sequence != 7 || envelope.EventID != 9 || envelope.PayloadLength != 6 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}
func TestRejectsInvalidTLV(t *testing.T) {
	raw, _ := hex.DecodeString("4c5354500102000007000000090000000600000074ddc48900000200aabb4f579013")
	if _, err := Validate(raw); err == nil {
		t.Fatal("expected invalid TLV")
	}
}
