// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package ingest

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/laststate/relay/internal/store"
)

func TestAcceptRejectsCorruptAndStoresValid(t *testing.T) {
	db, err := store.OpenWithOptions(t.TempDir(), store.Options{
		MaxSpoolBytes: 10 << 20,
		MinFreeBytes:  1,
		FsyncMode:     "none",
		DeliveryMode:  "local-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := Service{Store: db}

	if _, err := service.Accept(context.Background(), "s", []byte("nope")); err == nil {
		t.Fatal("expected corrupt")
	} else if Code(err) != CodeCorrupt && Code(err) != CodeUnsupported {
		// short buffer is corrupt
		if Code(err) == CodeInternal {
			t.Fatalf("code=%s err=%v", Code(err), err)
		}
	}

	raw, err := hex.DecodeString("4c5354500102000007000000090000000600000074ddc48901000200aabbea84ccd8")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Accept(context.Background(), "s", raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Duplicate || result.Event.ID == "" {
		t.Fatalf("%#v", result)
	}
}
