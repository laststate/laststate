// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package adapter runs external transport helpers that emit length-prefixed frames on stdout.
package adapter

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// FrameHandler receives raw payloads from an adapter process.
type FrameHandler func(payload []byte) error

// Subprocess runs an external adapter that emits length-prefixed frames on stdout.
type Subprocess struct {
	Command string
	Args    []string
	Env     []string
	Dir     string
	OnFrame FrameHandler
	OnLog   func(line string)

	cmd    *exec.Cmd
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (s *Subprocess) Start(ctx context.Context) error {
	if s.Command == "" {
		return fmt.Errorf("adapter command is required")
	}
	if s.OnFrame == nil {
		return fmt.Errorf("adapter frame handler is required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.cmd = exec.CommandContext(runCtx, s.Command, s.Args...) //nolint:gosec // command from operator config, not input
	if len(s.Env) > 0 {
		s.cmd.Env = append(os.Environ(), s.Env...)
	}
	s.cmd.Dir = s.Dir
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := s.cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := s.cmd.Start(); err != nil {
		cancel()
		return err
	}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		_ = readFrames(stdout, s.OnFrame)
	}()
	go func() {
		defer s.wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 && s.OnLog != nil {
				s.OnLog(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()
	return nil
}

func (s *Subprocess) Wait() error {
	if s.cmd == nil {
		return nil
	}
	err := s.cmd.Wait()
	s.wg.Wait()
	return err
}

func (s *Subprocess) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func readFrames(r io.Reader, handle FrameHandler) error {
	for {
		var length uint32
		if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if length == 0 || length > 16<<20 {
			return fmt.Errorf("invalid adapter frame length %d", length)
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return err
		}
		if err := handle(payload); err != nil {
			return err
		}
	}
}

// WriteFrame encodes a length-prefixed frame (for tests and sample adapters).
func WriteFrame(w io.Writer, payload []byte) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
