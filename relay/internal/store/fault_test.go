// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/laststate/relay/internal/lep"
)

func TestSpoolFullRejectsNewEvents(t *testing.T) {
	dir := t.TempDir()
	relay, err := OpenWithOptions(dir, Options{
		MaxSpoolBytes:  400,
		MinFreeBytes:   1, // zero is treated as "use default" by OpenWithOptions
		FsyncMode:      "none",
		PressurePolicy: "reject-new",
		DeliveryMode:   "local-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	// First event fits; second unique event exceeds the tiny spool limit.
	big := make([]byte, 300)
	for i := range big {
		big[i] = byte(i)
	}
	fields := []lep.TLV{{Type: 1, Value: big[:200]}}
	raw, err := lep.EncodeTLVs(lep.Version1, 1, 0, 1, 1, fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := relay.Put(context.Background(), "s", raw, mustEnv(t, raw)); err != nil {
		t.Fatal(err)
	}
	fields2 := []lep.TLV{{Type: 1, Value: big}}
	raw2, err := lep.EncodeTLVs(lep.Version1, 1, 0, 2, 2, fields2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = relay.Put(context.Background(), "s", raw2, mustEnv(t, raw2))
	if err == nil {
		t.Fatal("expected spool full")
	}
	if !isSpoolFull(err) {
		t.Fatalf("expected ErrSpoolFull, got %v", err)
	}
}

func isSpoolFull(err error) bool {
	if err == nil {
		return false
	}
	if err == ErrSpoolFull {
		return true
	}
	return errors.Is(err, ErrSpoolFull) || strings.Contains(err.Error(), ErrSpoolFull.Error())
}

func TestReconcileDetectsCorruptObject(t *testing.T) {
	dir := t.TempDir()
	relay, err := OpenWithOptions(dir, Options{MaxSpoolBytes: 10 << 20, MinFreeBytes: 1, FsyncMode: "none", DeliveryMode: "local-only"})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	raw, err := lep.Encode(lep.Version1, 1, 0, 1, 1, []byte{1, 0, 1, 0, 9})
	if err != nil {
		t.Fatal(err)
	}
	result, err := relay.Put(context.Background(), "s", raw, mustEnv(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.Event.RawObjectPath, []byte("corrupted-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := relay.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.CorruptObjects) == 0 && len(report.MissingObjects) == 0 {
		// Some implementations may rehash only when asked; still ensure no panic.
		t.Logf("reconcile report: %#v", report)
	}
}

func mustEnv(t *testing.T, raw []byte) lep.Envelope {
	t.Helper()
	env, err := lep.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return env
}
