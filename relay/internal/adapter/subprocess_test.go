// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package adapter

import (
	"bytes"
	"testing"
)

func TestWriteReadFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	var got []byte
	if err := readFrames(&buf, func(payload []byte) error {
		got = append([]byte(nil), payload...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}
