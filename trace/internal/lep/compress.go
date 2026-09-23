package lep

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
)

// decompressPayload tries gzip then zlib (common embedded choices).
// Format prefix: optional 1-byte algorithm id (1=gzip, 2=zlib, 0/raw=auto).
func decompressPayload(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return payload, nil
	}
	// Auto-detect gzip magic
	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		return gunzip(payload)
	}
	// Optional type byte
	if payload[0] == 1 && len(payload) > 1 {
		return gunzip(payload[1:])
	}
	if payload[0] == 2 && len(payload) > 1 {
		return inflate(payload[1:])
	}
	// try zlib
	if out, err := inflate(payload); err == nil {
		return out, nil
	}
	if out, err := gunzip(payload); err == nil {
		return out, nil
	}
	return nil, fmt.Errorf("unknown compression")
}

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, MaxEnvelopeSize))
}

func inflate(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, MaxEnvelopeSize))
}

// CompressGzip is a helper for tests/producers.
func CompressGzip(plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
