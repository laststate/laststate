//go:build !linux

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package source

import (
	"context"
	"fmt"
	"net"
	"time"
)

// canSocket is defined on all platforms so the shared read loop can type
// assert for native SocketCAN sockets; on non-Linux builds it is never
// instantiated and just wraps a generic net.Conn.
type canSocket struct {
	net.Conn
}

// dialSocketCAN is the non-Linux fallback for a local CAN interface. Native
// AF_CAN sockets are Linux-only; on other platforms we attempt a best-effort
// connection to a vcan/can device exposed over a unixgram socket so that
// development setups (e.g. shared CAN-over-TCP bridges) keep working.
func dialSocketCAN(ctx context.Context, iface string) (net.Conn, error) {
	if iface == "" {
		return nil, fmt.Errorf("socketcan: interface name is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := "/dev/" + iface
	conn, err := net.DialTimeout("unixgram", path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("socketcan %s (non-linux fallback): %w", iface, err)
	}
	return conn, nil
}
