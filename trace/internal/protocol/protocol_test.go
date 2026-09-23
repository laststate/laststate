package protocol_test

import (
	"testing"

	"github.com/laststate/trace/internal/protocol"
)

func TestReexportEncodeValidate(t *testing.T) {
	raw, err := protocol.Encode(protocol.Header{Type: protocol.TypeHealth, Architecture: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := protocol.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != protocol.TypeHealth {
		t.Fatal()
	}
	if protocol.EventTypeName(protocol.TypeCrash) != "crash" {
		t.Fatal()
	}
	if protocol.Magic != "LSTP" {
		t.Fatal()
	}
	if protocol.TLVBootID == 0 || protocol.TLVAttachment == 0 {
		t.Fatal()
	}
}
