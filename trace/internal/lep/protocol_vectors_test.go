package lep_test

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laststate/trace/internal/lep"
)

// TestProtocolVectors runs laststate/protocol goldens (vendored under testdata/).
// Structural kinds (valid/invalid/crypto/stream/lsak) are validated here; the
// reference Go codec in the protocol repo verifies the AEAD/HMAC tags against
// the documented test keys. PROTOCOL_VECTORS overrides the root when set
// (e.g. monorepo checkout).
func TestProtocolVectors(t *testing.T) {
	root := protocolVectorsRoot()
	if root == "" {
		t.Fatal("protocol test-vectors not found (expected testdata/protocol-vectors)")
	}
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Vectors []struct {
			ID      string `json:"id"`
			Path    string `json:"path"`
			Kind    string `json:"kind"`
			EventID uint32 `json:"event_id"`
			Status  uint8  `json:"status"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Vectors) == 0 {
		t.Fatal("empty manifest")
	}
	for _, v := range manifest.Vectors {
		v := v
		t.Run(v.ID, func(t *testing.T) {
			hexBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(v.Path)))
			if err != nil {
				t.Fatal(err)
			}
			data, err := hex.DecodeString(strings.TrimSpace(string(hexBytes)))
			if err != nil {
				t.Fatal(err)
			}
			switch v.Kind {
			case "valid", "crypto-aead", "crypto-hmac":
				if _, verr := lep.Validate(data); verr != nil {
					t.Fatalf("want valid (%s): %v", v.Kind, verr)
				}
			case "stream":
				frame, serr := parseStreamFrame(data)
				if serr != nil {
					t.Fatalf("parse stream: %v", serr)
				}
				if _, verr := lep.Validate(frame); verr != nil {
					t.Fatalf("stream envelope valid: %v", verr)
				}
			case "lsak":
				eventID, status, lerr := parseLSAK(data)
				if lerr != nil {
					t.Fatalf("parse lsak: %v", lerr)
				}
				if eventID != v.EventID || status != v.Status {
					t.Fatalf("lsak (%d,%d), want (%d,%d)", eventID, status, v.EventID, v.Status)
				}
			case "invalid":
				if _, verr := lep.Validate(data); verr == nil {
					t.Fatal("want invalid")
				}
			default:
				t.Fatalf("unknown kind %q", v.Kind)
			}
		})
	}
}

// parseStreamFrame extracts a single Latch Stream envelope: "LS", version,
// flags, lep_length u32 LE, envelope, CRC-32/IEEE of the envelope.
func parseStreamFrame(data []byte) ([]byte, error) {
	if len(data) < 8 || string(data[:2]) != "LS" || data[2] != 1 || data[3] != 0 {
		return nil, os.ErrInvalid
	}
	n := int(binary.LittleEndian.Uint32(data[4:8]))
	if len(data) != 8+n+4 {
		return nil, os.ErrInvalid
	}
	if binary.LittleEndian.Uint32(data[8+n:]) != crc32.ChecksumIEEE(data[8:8+n]) {
		return nil, os.ErrInvalid
	}
	return data[8 : 8+n], nil
}

// parseLSAK reads the 12-byte LSAK control message: magic, version, status,
// reserved, event_id.
func parseLSAK(data []byte) (uint32, uint8, error) {
	if len(data) != 12 || string(data[:4]) != "LSAK" || data[4] != 1 {
		return 0, 0, os.ErrInvalid
	}
	return binary.LittleEndian.Uint32(data[8:12]), data[5], nil
}

func protocolVectorsRoot() string {
	if p := os.Getenv("PROTOCOL_VECTORS"); p != "" {
		if st, err := os.Stat(filepath.Join(p, "manifest.json")); err == nil && !st.IsDir() {
			return p
		}
	}
	// Prefer vendored copy (cwd = package dir during go test).
	candidates := []string{
		filepath.Join("testdata", "protocol-vectors"),
		filepath.Join("..", "..", "..", "protocol", "test-vectors"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "manifest.json")); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}
