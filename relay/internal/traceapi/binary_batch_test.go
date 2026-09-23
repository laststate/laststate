// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package traceapi

import "testing"

func TestBinaryBatchRoundTrip(t *testing.T) {
	events := []BatchEvent{
		{EventID: "evt_a", Payload: []byte{1, 2, 3}},
		{EventID: "evt_b", Payload: []byte{4, 5}},
	}
	raw, err := EncodeBinaryBatch(events)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeBinaryBatch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].EventID != "evt_a" || string(out[1].Payload) != string([]byte{4, 5}) {
		t.Fatalf("%#v", out)
	}
}
