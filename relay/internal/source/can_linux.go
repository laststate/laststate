//go:build linux

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package source

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// canSocket is a native AF_CAN socket that satisfies net.Conn. SocketCAN
// frames are read in their raw kernel layout (struct can_frame for classic
// CAN, struct canfd_frame for CAN FD) and exposed as net.Conn reads so the
// shared RunCAN read loop keeps working unchanged.
type canSocket struct {
	fd       int
	mu       sync.Mutex
	deadline time.Time
}

func (s *canSocket) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deadline.IsZero() && time.Now().After(s.deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	return unix.Read(s.fd, b)
}

func (s *canSocket) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return unix.Write(s.fd, b)
}

func (s *canSocket) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return unix.Close(s.fd)
}

func (s *canSocket) LocalAddr() net.Addr  { return canAddr{} }
func (s *canSocket) RemoteAddr() net.Addr { return canAddr{} }

func (s *canSocket) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deadline = t
	return nil
}

func (s *canSocket) SetReadDeadline(t time.Time) error { return s.SetDeadline(t) }
func (s *canSocket) SetWriteDeadline(t time.Time) error {
	return s.SetDeadline(t)
}

type canAddr struct{}

func (canAddr) Network() string { return "can" }
func (canAddr) String() string  { return "socketcan" }

// dialSocketCAN opens a native SocketCAN socket bound to the given interface
// (e.g. "can0"). It returns the interface name unchanged; the raw frame
// headers are decoded later by parseCANFrames.
func dialSocketCAN(ctx context.Context, iface string) (net.Conn, error) {
	if iface == "" {
		return nil, fmt.Errorf("socketcan: interface name is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iface = sanitizeSocketCANName(iface)
	idx, err := canInterfaceIndex(iface)
	if err != nil {
		return nil, err
	}

	// Classic CAN socket first; CAN FD mode is negotiated via setsockopt when
	// the source requests it.
	sock, err := unix.Socket(unix.AF_CAN, unix.SOCK_RAW, unix.CAN_RAW)
	if err != nil {
		return nil, fmt.Errorf("socketcan %s: socket: %w", iface, err)
	}
	if err := unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_RCVTIMEO_OLD, int(5*time.Second/time.Millisecond)); err != nil {
		_ = unix.Close(sock)
		return nil, fmt.Errorf("socketcan %s: setsockopt: %w", iface, err)
	}

	addr := &unix.SockaddrCAN{Ifindex: idx}
	if err := unix.Bind(sock, addr); err != nil {
		_ = unix.Close(sock)
		return nil, fmt.Errorf("socketcan %s: bind: %w", iface, err)
	}

	return &canSocket{fd: sock}, nil
}

// canInterfaceIndex resolves a CAN interface name to its ifindex.
func canInterfaceIndex(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("socketcan %s: %w", name, err)
	}
	return iface.Index, nil
}

// sanitizeSocketCANName normalizes an interface name. Callers may pass either
// "can0" or "/dev/can0"; only the bare name is valid for AF_CAN.
func sanitizeSocketCANName(name string) string {
	const prefix = "/dev/"
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		return name[len(prefix):]
	}
	return name
}
