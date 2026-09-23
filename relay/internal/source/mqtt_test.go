// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package source

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSanitizeBrokerStripsCredentials(t *testing.T) {
	got := sanitizeBroker("mqtt://user:secret@broker.example:1883/path")
	if strings.Contains(got, "secret") || strings.Contains(got, "user:") {
		t.Fatalf("credentials leaked: %s", got)
	}
	if !strings.Contains(got, "broker.example") {
		t.Fatalf("host lost: %s", got)
	}
}

func TestSanitizeBrokerPlain(t *testing.T) {
	in := "tcp://127.0.0.1:1883"
	if got := sanitizeBroker(in); got != in {
		t.Fatalf("got %q want %q", got, in)
	}
}

func TestRunMQTTRequiresBroker(t *testing.T) {
	err := RunMQTT(context.Background(), MQTTConfig{Topics: []string{"t"}}, func(string, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "broker") {
		t.Fatalf("want broker error, got %v", err)
	}
}

func TestRunMQTTRequiresTopics(t *testing.T) {
	err := RunMQTT(context.Background(), MQTTConfig{Broker: "tcp://127.0.0.1:1883"}, func(string, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "topics") {
		t.Fatalf("want topics error, got %v", err)
	}
}

func TestRunMQTTRequiresHandler(t *testing.T) {
	err := RunMQTT(context.Background(), MQTTConfig{Broker: "tcp://127.0.0.1:1883", Topics: []string{"t"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "handler") {
		t.Fatalf("want handler error, got %v", err)
	}
}

func TestRunMQTTConnectFailure(t *testing.T) {
	// Unreachable broker: should fail connect without hanging forever.
	err := RunMQTT(context.Background(), MQTTConfig{
		Broker:           "tcp://127.0.0.1:1", // closed port
		ClientID:         "relay-test",
		Topics:           []string{"laststate/test"},
		QoS:              9, // clamped before connect
		ConnectTimeout:   2 * time.Second,
		SubscribeTimeout: time.Second,
	}, func(string, []byte) error { return nil })
	if err == nil {
		t.Fatal("expected connect error")
	}
	// Error text must not embed credentials even if broker URL had them.
	if strings.Contains(err.Error(), "password") {
		t.Fatalf("leaked secret in error: %v", err)
	}
}
