// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package privacy

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/laststate/relay/internal/lep"
)

func TestPolicyDropsTLVAndRecomputesIntegrity(t *testing.T) {
	payload := []byte{
		1, 0, 2, 0, 0xaa, 0xbb,
		2, 0, 1, 0, 0xcc,
	}
	raw := makeEnvelope(payload, 0)
	transformed, err := (Policy{DropTLVTypes: []uint16{1}}).Apply(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lep.Validate(transformed); err != nil {
		t.Fatalf("transformed envelope failed validation: %v", err)
	}
	fields, err := lep.TLVs(transformed)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0].Type != 2 || len(fields[0].Value) != 1 || fields[0].Value[0] != 0xcc {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	if string(raw) == string(transformed) {
		t.Fatal("policy unexpectedly returned the original envelope")
	}
}

func TestPolicyRefusesAuthenticatedRewrite(t *testing.T) {
	raw := makeEnvelope([]byte{1, 0, 1, 0, 9}, lep.FlagAuthenticated)
	if _, err := (Policy{DropTLVTypes: []uint16{1}}).Apply(raw); err == nil {
		t.Fatal("expected authenticated envelope rewrite to fail")
	}
}

func makeEnvelope(payload []byte, flags uint8) []byte {
	raw := make([]byte, lep.HeaderSize+len(payload)+4)
	copy(raw, lep.Magic)
	raw[4], raw[5], raw[7] = 1, 1, flags
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint32(raw[12:16], 2)
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[lep.HeaderSize:], payload)
	binary.LittleEndian.PutUint32(raw[lep.HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	return raw
}
