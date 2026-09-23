// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package config

import "testing"

func baseCfg(listen, token string) Config {
	return Config{
		Version:  1,
		Instance: Instance{DataDir: "./data"},
		Spool: Spool{
			MaxBytes:       1 << 20,
			MinFreeBytes:   1,
			PressurePolicy: "reject-new",
			Fsync:          "full",
		},
		Delivery: Delivery{Mode: "local-only"},
		Admin:    Admin{Listen: listen, Token: token},
		Privacy:  Privacy{PreserveRawLocally: true},
	}
}

func TestAdminTokenRequiredOffLoopback(t *testing.T) {
	if err := Validate(baseCfg("0.0.0.0:8383", "")); err == nil {
		t.Fatal("expected admin.token required for non-loopback listen")
	}
}

func TestAdminTokenOptionalOnLoopback(t *testing.T) {
	if err := Validate(baseCfg("127.0.0.1:8383", "")); err != nil {
		t.Fatal(err)
	}
}
