package worker

import (
	"testing"

	"github.com/laststate/trace/internal/lep"
)

func TestPipelineForType(t *testing.T) {
	cases := map[uint8]string{
		lep.TypeHealth:     "health",
		lep.TypeLog:        "log",
		lep.TypeMessage:    "log",
		lep.TypePeripheral: "metric",
		lep.TypeReset:      "boot",
		lep.TypeCrash:      "issue",
		lep.TypeError:      "issue",
		lep.TypeCoredump:   "issue",
		99:                 "issue",
	}
	for typ, want := range cases {
		if got := pipelineForType(typ); got != want {
			t.Fatalf("type %d got %s want %s", typ, got, want)
		}
	}
}

func TestSymbolizerVersionConst(t *testing.T) {
	if SymbolizerVersion < 1 {
		t.Fatal()
	}
}
