// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package traceapi holds Trace HTTP request/response types.
package traceapi

type BatchRequest struct {
	Events []BatchEvent `json:"events"`
}

type BatchEvent struct {
	EventID string `json:"event_id"`
	Payload []byte `json:"payload"`
}

type BatchResponse struct {
	Accepted   []AcceptedEvent `json:"accepted,omitempty"`
	Duplicates []AcceptedEvent `json:"duplicates,omitempty"`
	Rejected   []RejectedEvent `json:"rejected,omitempty"`
}

type AcceptedEvent struct {
	EventID string `json:"event_id"`
	Receipt string `json:"receipt,omitempty"`
}

type RejectedEvent struct {
	EventID   string `json:"event_id"`
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable"`
}
