package fingerprint_test

import (
	"strings"
	"testing"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/fingerprint"
	"github.com/laststate/trace/internal/lep"
)

func TestComputeStable(t *testing.T) {
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		PC:     0x08001234,
		Assert: "x != 0",
	}
	r := analysis.Report{ProbableCause: "null pointer", Frames: []analysis.Frame{{Address: 0x801234, Function: "main"}}}
	a := fingerprint.Compute(d, r)
	b := fingerprint.Compute(d, r)
	if a != b {
		t.Fatal("not stable")
	}
	if !strings.HasPrefix(a, "v1:") {
		t.Fatal(a)
	}
	// change cause → different fp
	r2 := r
	r2.ProbableCause = "other"
	if fingerprint.Compute(d, r2) == a {
		t.Fatal("expected different")
	}
}

func TestComputeWithDevFingerprint(t *testing.T) {
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeError, Architecture: 1},
		Event:  &decode.EventMeta{Fingerprint: 0xdead},
	}
	fp := fingerprint.Compute(d, analysis.Report{})
	if fp == "" {
		t.Fatal()
	}
}

func TestComputeWithFileLine(t *testing.T) {
	d := decode.Decoded{Header: lep.Header{Type: lep.TypeCrash, Architecture: 1}}
	r := analysis.Report{Frames: []analysis.Frame{{Function: "foo", File: "/a/b/c.c", Line: 10}}}
	fp := fingerprint.Compute(d, r)
	if !strings.HasPrefix(fp, fingerprint.Version+":") {
		t.Fatal(fp)
	}
}

func TestTitle(t *testing.T) {
	d := decode.Decoded{Header: lep.Header{Type: lep.TypeCrash}, PC: 0x100}
	if fingerprint.Title(d, analysis.Report{ProbableCause: "div0"}) != "div0" {
		t.Fatal()
	}
	if fingerprint.Title(d, analysis.Report{Frames: []analysis.Frame{{Function: "bar"}}}) != "bar" {
		t.Fatal()
	}
	title := fingerprint.Title(d, analysis.Report{})
	if !strings.Contains(title, "crash") {
		t.Fatal(title)
	}
}

func TestDifferentPCDifferentFP(t *testing.T) {
	d1 := decode.Decoded{Header: lep.Header{Type: lep.TypeCrash, Architecture: 1}, PC: 1}
	d2 := decode.Decoded{Header: lep.Header{Type: lep.TypeCrash, Architecture: 1}, PC: 2}
	if fingerprint.Compute(d1, analysis.Report{}) == fingerprint.Compute(d2, analysis.Report{}) {
		t.Fatal("pc should influence")
	}
}
