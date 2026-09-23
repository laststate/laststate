// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package ingest validates LEP frames and writes them to the spool.
package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/laststate/relay/internal/lep"
	"github.com/laststate/relay/internal/store"
)

type ErrorCode string

const (
	CodeCorrupt     ErrorCode = "corrupt"
	CodeUnsupported ErrorCode = "unsupported"
	CodeTooLarge    ErrorCode = "too_large"
	CodeBusy        ErrorCode = "busy"
	CodeInternal    ErrorCode = "internal"
	// CodeReplay is a recycled device event_id with non-identical payload bytes.
	CodeReplay ErrorCode = "replay"
)

type Error struct {
	Code ErrorCode
	Err  error
}

func (err *Error) Error() string { return fmt.Sprintf("%s: %v", err.Code, err.Err) }
func (err *Error) Unwrap() error { return err.Err }

func Code(err error) ErrorCode {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return CodeInternal
}

// Hooks are optional callbacks after accept or reject.
type Hooks struct {
	OnAccept func(sourceID string, result store.Result)
	OnError  func(sourceID string, err error)
}

// Router picks destination IDs in route delivery mode.
type Router func(sourceID string, envelope lep.Envelope) []string

// Service accepts LEP frames from all sources.
type Service struct {
	Store  *store.Store
	Hooks  Hooks
	Router Router

	Keyring            lep.Keyring
	Replay             *lep.ReplayCache
	VerifyCrypto       bool
	DecryptOnIngest    bool // Open crypto envelopes; stored bytes stay as received
	AllowOpaqueForward bool // allow store without Open when not verifying
}

// Accept validates, optionally checks crypto/replay, stores raw bytes, queues delivery.
func (service Service) Accept(ctx context.Context, sourceID string, raw []byte) (store.Result, error) {
	envelope, err := lep.Validate(raw)
	if err != nil {
		wrapped := classifyLEPError(err)
		service.notifyError(sourceID, wrapped)
		return store.Result{}, wrapped
	}

	if err := service.verifyCrypto(raw, envelope); err != nil {
		wrapped := &Error{Code: CodeCorrupt, Err: err}
		service.notifyError(sourceID, wrapped)
		return store.Result{}, wrapped
	}

	if service.Replay != nil {
		if _, conflict := service.Replay.Check(sourceID, envelope.EventID, raw); conflict {
			wrapped := &Error{Code: CodeReplay, Err: fmt.Errorf("replay of event_id %d from source %s with different payload", envelope.EventID, sourceID)}
			service.notifyError(sourceID, wrapped)
			return store.Result{}, wrapped
		}
	}

	var targets []string
	if service.Router != nil {
		targets = service.Router(sourceID, envelope)
	}
	// Put queues destinations in the same SQLite transaction as the event row.
	result, err := service.Store.Put(ctx, sourceID, raw, envelope, targets...)
	if err != nil {
		wrapped := &Error{Code: CodeInternal, Err: err}
		if errors.Is(err, store.ErrSpoolFull) {
			wrapped.Code = CodeBusy
		}
		service.notifyError(sourceID, wrapped)
		return result, wrapped
	}
	if service.Replay != nil && !result.Duplicate {
		service.Replay.Record(sourceID, envelope.EventID, raw)
	}
	if service.Hooks.OnAccept != nil {
		service.Hooks.OnAccept(sourceID, result)
	}
	return result, nil
}

func (service Service) verifyCrypto(raw []byte, envelope lep.Envelope) error {
	cryptoFlags := envelope.Flags & (lep.FlagAuthenticated | lep.FlagEncrypted | lep.FlagAEAD)
	if cryptoFlags == 0 {
		return nil
	}
	mustVerify := service.VerifyCrypto || service.DecryptOnIngest || (service.Keyring != nil && !service.AllowOpaqueForward)
	if !mustVerify {
		return nil
	}
	if service.Keyring == nil {
		return fmt.Errorf("authenticated or encrypted envelope requires a configured keyring")
	}
	if _, err := lep.Open(raw, service.Keyring); err != nil {
		return fmt.Errorf("crypto verification failed: %w", err)
	}
	return nil
}

func (service Service) notifyError(sourceID string, err error) {
	if service.Hooks.OnError != nil {
		service.Hooks.OnError(sourceID, err)
	}
}

func classifyLEPError(err error) *Error {
	code := CodeCorrupt
	var validation *lep.ValidationError
	if errors.As(err, &validation) {
		switch validation.Kind {
		case lep.ErrorUnsupported:
			code = CodeUnsupported
		case lep.ErrorTooLarge:
			code = CodeTooLarge
		}
	}
	return &Error{Code: code, Err: fmt.Errorf("validate LEP: %w", err)}
}
