// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// gRPC LoRaWAN transport.
//
// The gRPC transport lets a LoRaWAN network server (or any gateway) push raw
// LEP frames to the relay over a bidirectional stream. It deliberately avoids
// protobuf-generated stubs so the repository builds without `protoc`: the
// service is described with a hand-written grpc.ServiceDesc and a raw byte
// codec that carries each LEP frame verbatim.
//
// Wire contract:
//
//	service : laststate.relay.lorawan.v1.Uplink
//	method  : /laststate.relay.lorawan.v1.Uplink/Stream (bidirectional stream)
//	codec   : content-subtype "laststate-lep-frame" (raw bytes, no protobuf)
//	request : one message == one raw LEP frame ([]byte)
//	response: one message per request — "ACK_STORED" once the frame is
//	          persisted (persist-before-ACK), or "NACK <reason>" if ingest
//	          rejected it. The stream stays open either way.
//
// The relay binds the gRPC listener to the configured `lorawan.server`
// address (host:port). Every accepted frame flows through the same ingest
// pipeline as the other transports, so durability and idempotency guarantees
// are identical.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const (
	// GRPCFrameCodecName is the content-subtype of the raw LEP frame codec.
	// It carries LEP frames as opaque bytes so no protobuf schema is required.
	GRPCFrameCodecName = "laststate-lep-frame"

	// GRPCServiceName is the fully-qualified gRPC service name for uplinks.
	GRPCServiceName = "laststate.relay.lorawan.v1.Uplink"

	// GRPCStreamName is the bidirectional streaming method name.
	GRPCStreamName = "Stream"

	// GRPCStreamMethod is the full method path clients dial.
	GRPCStreamMethod = "/" + GRPCServiceName + "/" + GRPCStreamName

	// grpcMaxFrameBytes bounds a single received LEP frame. Frames larger than
	// this are rejected without allocating, matching the relay's rule to never
	// size buffers from unvalidated network input.
	grpcMaxFrameBytes = 1 << 20
)

// grpcAckStored is returned to the client only after a frame has been handed
// to ingest (written and committed to the spool).
var grpcAckStored = []byte("ACK_STORED")

// grpcFrameCodec is a gRPC encoding.Codec that passes LEP frames through as
// raw bytes. It removes the dependency on protobuf-generated message types.
type grpcFrameCodec struct{}

func (grpcFrameCodec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case []byte:
		return m, nil
	case *[]byte:
		if m == nil {
			return nil, nil
		}
		return *m, nil
	default:
		return nil, fmt.Errorf("lorawan grpc: raw codec cannot marshal %T", v)
	}
}

func (grpcFrameCodec) Unmarshal(data []byte, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("lorawan grpc: raw codec cannot unmarshal into %T", v)
	}
	buf := make([]byte, len(data))
	copy(buf, data)
	*p = buf
	return nil
}

func (grpcFrameCodec) Name() string { return GRPCFrameCodecName }

func init() {
	// Registering globally lets both the server and any in-process client
	// select the raw codec via the negotiated content-subtype.
	encoding.RegisterCodec(grpcFrameCodec{})
}

// grpcNack formats a negative acknowledgement carrying a short reason.
func grpcNack(reason string) []byte {
	return []byte("NACK " + reason)
}

// lorawanGRPCServer receives raw LEP frames over a gRPC bidirectional stream
// and forwards each frame to the ingest handler before acknowledging the
// client, preserving the relay's persist-before-ACK contract.
type lorawanGRPCServer struct {
	log    *slog.Logger
	cfg    LoRaWANConfig
	handle LoRaWANHandler
}

// register attaches the hand-written service description to srv.
func (s *lorawanGRPCServer) register(srv *grpc.Server) {
	desc := grpc.ServiceDesc{
		ServiceName: GRPCServiceName,
		// HandlerType is only used by grpc for a reflective interface check.
		// The empty interface accepts our concrete server, so no protobuf
		// service interface has to be generated.
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName:    GRPCStreamName,
			Handler:       s.handleStream,
			ServerStreams: true,
			ClientStreams: true,
		}},
		Metadata: "laststate/relay/lorawan/grpc",
	}
	srv.RegisterService(&desc, s)
}

// handleStream reads LEP frames until the client half-closes or ctx ends.
func (s *lorawanGRPCServer) handleStream(_ any, stream grpc.ServerStream) error {
	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var frame []byte
		if err := stream.RecvMsg(&frame); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if len(frame) == 0 {
			if err := stream.SendMsg(grpcNack("empty frame")); err != nil {
				return err
			}
			continue
		}
		if len(frame) > grpcMaxFrameBytes {
			if err := stream.SendMsg(grpcNack("frame too large")); err != nil {
				return err
			}
			continue
		}

		uplink := LoRaWANUplink{
			DeviceEUI: s.cfg.DeviceEUI,
			Port:      s.cfg.Port,
			Data:      frame,
			Timestamp: time.Now(),
		}

		// persist-before-ACK: handle() drives ingest, which writes the raw LEP
		// frame and commits the SQLite transaction before this call returns.
		// Only then do we acknowledge the client.
		if err := s.handle(uplink, frame); err != nil {
			s.log.Warn("LoRaWAN gRPC frame rejected", "error", err)
			if sErr := stream.SendMsg(grpcNack(err.Error())); sErr != nil {
				return sErr
			}
			continue
		}

		ack := append([]byte(nil), grpcAckStored...)
		if err := stream.SendMsg(&ack); err != nil {
			return err
		}
	}
}

// runGRPC binds a gRPC listener on the configured server address and serves
// the raw-frame uplink stream until ctx is cancelled.
func (c *LoRaWANClient) runGRPC(ctx context.Context, handle LoRaWANHandler) error {
	if c.config.Server == "" {
		return fmt.Errorf("lorawan: gRPC listen address is required")
	}
	lis, err := net.Listen("tcp", c.config.Server)
	if err != nil {
		return fmt.Errorf("lorawan: gRPC listen %s: %w", sanitizeBroker(c.config.Server), err)
	}
	if c.config.TLSEnabled {
		// Server-side TLS needs a certificate/key pair that the LoRaWAN config
		// does not carry. Bind to loopback or terminate TLS in front of the
		// relay instead of assuming an insecure listener is fine.
		c.log.Warn("LoRaWAN gRPC TLS requested but no server certificate is configured; serving without transport security (bind to loopback)")
	}
	return c.serveGRPC(ctx, lis, handle)
}

// serveGRPC runs the gRPC server on lis. It is separated from runGRPC so tests
// can drive it over an in-process bufconn listener.
func (c *LoRaWANClient) serveGRPC(ctx context.Context, lis net.Listener, handle LoRaWANHandler) error {
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(grpcMaxFrameBytes))
	server := &lorawanGRPCServer{log: c.log, cfg: c.config, handle: handle}
	server.register(srv)

	c.log.Info("LoRaWAN gRPC server listening",
		"addr", lis.Addr().String(),
		"service", GRPCServiceName,
		"method", GRPCStreamMethod)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(lis) }()

	select {
	case <-ctx.Done():
		srv.GracefulStop()
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("lorawan: gRPC serve: %w", err)
		}
		return err
	}
}

// LoRaWANGRPCStream is a minimal client for the raw-frame uplink service. It is
// used by tests and by gateways that push LEP frames into the relay.
type LoRaWANGRPCStream struct {
	stream grpc.ClientStream
}

// NewLoRaWANGRPCStream opens the bidirectional uplink stream over conn.
func NewLoRaWANGRPCStream(ctx context.Context, conn *grpc.ClientConn) (*LoRaWANGRPCStream, error) {
	desc := &grpc.StreamDesc{StreamName: GRPCStreamName, ServerStreams: true, ClientStreams: true}
	stream, err := conn.NewStream(ctx, desc, GRPCStreamMethod, grpc.CallContentSubtype(GRPCFrameCodecName))
	if err != nil {
		return nil, err
	}
	return &LoRaWANGRPCStream{stream: stream}, nil
}

// SendFrame pushes one raw LEP frame and blocks for the server acknowledgement.
// The returned bytes are "ACK_STORED" once the frame is persisted, or
// "NACK <reason>" if ingest rejected it.
func (c *LoRaWANGRPCStream) SendFrame(frame []byte) ([]byte, error) {
	if err := c.stream.SendMsg(&frame); err != nil {
		return nil, err
	}
	var ack []byte
	if err := c.stream.RecvMsg(&ack); err != nil {
		return nil, err
	}
	return ack, nil
}

// Close half-closes the client side of the stream.
func (c *LoRaWANGRPCStream) Close() error {
	return c.stream.CloseSend()
}
