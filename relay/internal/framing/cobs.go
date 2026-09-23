// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package framing implements COBS framing for stream sources.
package framing

import (
	"fmt"
	"strings"
)

type COBS struct {
	buffer []byte
	max    int
}

type DecodeErrors []error

func (errs DecodeErrors) Error() string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

func NewCOBS(max int) *COBS {
	if max <= 0 {
		max = 4 << 20
	}
	return &COBS{max: max}
}

// Push continues after malformed frames and returns valid frames that followed
// them in the same byte stream.
func (framer *COBS) Push(data []byte) (frames [][]byte, err error) {
	var problems DecodeErrors
	for _, value := range data {
		if value != 0 {
			if len(framer.buffer) >= framer.max {
				framer.buffer = nil
				problems = append(problems, fmt.Errorf("COBS frame exceeds %d bytes", framer.max))
				continue
			}
			framer.buffer = append(framer.buffer, value)
			continue
		}
		if len(framer.buffer) == 0 {
			continue
		}
		decoded, decodeErr := Decode(framer.buffer)
		framer.buffer = nil
		if decodeErr != nil {
			problems = append(problems, decodeErr)
			continue
		}
		frames = append(frames, decoded)
	}
	if len(problems) > 0 {
		return frames, problems
	}
	return frames, nil
}

func (framer *COBS) Reset()        { framer.buffer = nil }
func (framer *COBS) Buffered() int { return len(framer.buffer) }

func Decode(encoded []byte) ([]byte, error) {
	decoded := make([]byte, 0, len(encoded))
	for offset := 0; offset < len(encoded); {
		code := int(encoded[offset])
		offset++
		if code == 0 || offset+code-1 > len(encoded) {
			return nil, fmt.Errorf("invalid COBS frame")
		}
		decoded = append(decoded, encoded[offset:offset+code-1]...)
		offset += code - 1
		if code < 0xff && offset < len(encoded) {
			decoded = append(decoded, 0)
		}
	}
	return decoded, nil
}

// Encode appends the 0x00 frame delimiter expected by COBS streams.
func Encode(data []byte) []byte {
	encoded := make([]byte, 1, len(data)+len(data)/254+2)
	codeIndex := 0
	code := byte(1)
	for _, value := range data {
		if value == 0 {
			encoded[codeIndex] = code
			codeIndex = len(encoded)
			encoded = append(encoded, 0)
			code = 1
			continue
		}
		encoded = append(encoded, value)
		code++
		if code == 0xff {
			encoded[codeIndex] = code
			codeIndex = len(encoded)
			encoded = append(encoded, 0)
			code = 1
		}
	}
	encoded[codeIndex] = code
	return append(encoded, 0)
}
