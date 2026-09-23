// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package source implements optional collectors such as MQTT.
package source

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTConfig describes a durable MQTT subscription that yields raw LEP payloads.
type MQTTConfig struct {
	Broker           string
	ClientID         string
	Topics           []string
	QoS              byte
	Username         string
	Password         string
	TLSConfig        *tls.Config
	ConnectTimeout   time.Duration // default 30s
	SubscribeTimeout time.Duration // default 15s
}

// MQTTHandler is invoked for each message payload.
type MQTTHandler func(topic string, payload []byte) error

// RunMQTT connects, subscribes (with reconnect), and blocks until ctx is cancelled.
func RunMQTT(ctx context.Context, cfg MQTTConfig, handle MQTTHandler) error {
	if cfg.Broker == "" {
		return fmt.Errorf("mqtt broker is required")
	}
	if len(cfg.Topics) == 0 {
		return fmt.Errorf("mqtt topics are required")
	}
	if handle == nil {
		return fmt.Errorf("mqtt handler is required")
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "laststate-relay"
	}
	if cfg.QoS > 2 {
		cfg.QoS = 1
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetOrderMatters(false)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	if cfg.TLSConfig != nil {
		opts.SetTLSConfig(cfg.TLSConfig)
	} else if strings.HasPrefix(cfg.Broker, "ssl://") || strings.HasPrefix(cfg.Broker, "tls://") || strings.HasPrefix(cfg.Broker, "mqtts://") {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	opts.SetDefaultPublishHandler(func(_ mqtt.Client, msg mqtt.Message) {
		_ = handle(msg.Topic(), msg.Payload())
	})

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 30 * time.Second
	}
	subscribeTimeout := cfg.SubscribeTimeout
	if subscribeTimeout <= 0 {
		subscribeTimeout = 15 * time.Second
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(connectTimeout) {
		return fmt.Errorf("mqtt connect timeout for %s", sanitizeBroker(cfg.Broker))
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer client.Disconnect(250)

	for _, topic := range cfg.Topics {
		token := client.Subscribe(topic, cfg.QoS, func(_ mqtt.Client, msg mqtt.Message) {
			_ = handle(msg.Topic(), msg.Payload())
		})
		if !token.WaitTimeout(subscribeTimeout) {
			return fmt.Errorf("mqtt subscribe timeout for %s", topic)
		}
		if err := token.Error(); err != nil {
			return fmt.Errorf("mqtt subscribe %s: %w", topic, err)
		}
	}

	<-ctx.Done()
	return nil
}

func sanitizeBroker(broker string) string {
	parsed, err := url.Parse(broker)
	if err != nil {
		return broker
	}
	parsed.User = nil
	return parsed.String()
}
