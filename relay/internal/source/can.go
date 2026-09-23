// Package source implements SocketCAN and CAN FD collectors for the relay.
//
// CAN sources connect to a local CAN interface (e.g. /dev/can0, can0) or
// a remote CAN-over-USB adapter and publish raw CAN frames as LEP payloads
// through the relay's ingest pipeline.
//
// Configuration:
//
//	CAN_INTERFACE=can0
//	CAN_BAUDRATE=500000
//	CAN_FD_ENABLED=true
//	CAN_FD_BRS=true
//	CAN_FILTER_ID=0x123
//	CAN_FILTER_MASK=0x7FF
//	CAN_PROTOCOL=LEP|JSON|RAW
//	CAN_ADAPTER_PATH=/dev/ttyUSB0
package source

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CANConfig describes a CAN source.
type CANConfig struct {
	Interface   string
	Baudrate    int
	FDEnabled   bool
	BRS         bool
	FilterID    uint32
	FilterMask  uint32
	Protocol    string // "LEP", "JSON", "RAW"
	AdapterPath string
	RemoteHost  string
	RemotePort  int
}

// CANFrame represents a CAN frame (standard or FD).
type CANFrame struct {
	ID        uint32
	DLC       uint8
	Data      []byte
	IsFD      bool
	BRS       bool
	IDE       bool
	Timestamp time.Time
}

// CANHandler is invoked for each CAN frame.
type CANHandler func(frame CANFrame, payload []byte) error

// RunCAN connects to a CAN interface and blocks until ctx is cancelled.
// It supports both local socketcan interfaces and remote CAN-over-TCP adapters.
func RunCAN(ctx context.Context, cfg CANConfig, handle CANHandler) error {
	if cfg.Interface == "" && cfg.AdapterPath == "" && cfg.RemoteHost == "" {
		return fmt.Errorf("can: at least one of Interface, AdapterPath, or RemoteHost must be set")
	}

	if cfg.Protocol == "" {
		cfg.Protocol = "LEP"
	}
	if cfg.Baudrate == 0 {
		cfg.Baudrate = 500000
	}

	log := slog.Default()

	// Connect to the CAN source
	conn, err := connectCANSource(ctx, cfg)
	if err != nil {
		return fmt.Errorf("can: connect: %w", err)
	}
	defer conn.Close()

	log.Info("CAN source connected",
		"interface", cfg.Interface,
		"baudrate", cfg.Baudrate,
		"fd", cfg.FDEnabled,
		"protocol", cfg.Protocol)

	// Filter setup
	if cfg.FilterID != 0 && cfg.FilterMask != 0 {
		log.Info("CAN filter configured",
			"id", fmt.Sprintf("0x%03X", cfg.FilterID),
			"mask", fmt.Sprintf("0x%03X", cfg.FilterMask))
	}

	// Read loop
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := conn.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warn("CAN read error, retrying", "error", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Native SocketCAN sockets deliver one raw kernel frame per read;
		// TCP/unix bridges stream a concatenated LEP or raw framing stream.
		if _, ok := conn.(*canSocket); ok {
			frame := decodeCANFrameLinux(buf[:n])
			if cfg.FilterID != 0 && cfg.FilterMask != 0 {
				if frame.ID&cfg.FilterMask != cfg.FilterID {
					continue
				}
			}
			payload := encodeCANFrame(frame, cfg.Protocol)
			if err := handle(frame, payload); err != nil {
				log.Warn("CAN handler error", "error", err)
			}
			continue
		}

		frames := parseCANFrames(buf[:n], cfg)
		for _, frame := range frames {
			payload := encodeCANFrame(frame, cfg.Protocol)
			if err := handle(frame, payload); err != nil {
				log.Warn("CAN handler error", "error", err)
			}
		}
	}
}

// connectCANSource establishes a connection to the CAN source. SocketCAN
// support (native AF_CAN sockets) lives in can_linux.go and is only compiled
// on Linux; other platforms fall back to remote TCP adapters.
func connectCANSource(ctx context.Context, cfg CANConfig) (net.Conn, error) {
	if cfg.RemoteHost != "" {
		// Remote CAN-over-TCP adapter
		addr := net.JoinHostPort(cfg.RemoteHost, strconv.Itoa(cfg.RemotePort))
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("remote CAN connect: %w", err)
		}
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		return conn, nil
	}

	if cfg.AdapterPath != "" {
		// USB-CAN adapter via serial (classic PTY / TCP socket)
		conn, err := net.DialTimeout("unix", cfg.AdapterPath, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("USB-CAN adapter: %w", err)
		}
		return conn, nil
	}

	if cfg.Interface != "" {
		// Local SocketCAN interface. Native AF_CAN sockets are implemented in
		// can_linux.go; other platforms attempt a best-effort unixgram link.
		return dialSocketCAN(ctx, cfg.Interface)
	}

	return nil, fmt.Errorf("no CAN source configured")
}

// parseCANFrames parses raw bytes into CAN frames.
func parseCANFrames(data []byte, cfg CANConfig) []CANFrame {
	var frames []CANFrame

	if cfg.Protocol == "LEP" {
		// LEP-encoded CAN frames: [id:4][flags:1][len:1][data:N]
		for len(data) >= 6 {
			if int(data[5]) > 64 || len(data) < 6+int(data[5]) {
				break
			}
			frame := CANFrame{
				ID:        binary.LittleEndian.Uint32(data[:4]),
				DLC:       data[4] & 0x0F,
				Data:      make([]byte, data[5]),
				IsFD:      data[4]&0x80 != 0,
				BRS:       data[4]&0x40 != 0,
				IDE:       data[4]&0x20 != 0,
				Timestamp: time.Now(),
			}
			copy(frame.Data, data[6:6+int(data[5])])
			frames = append(frames, frame)
			data = data[6+int(data[5]):]
		}
	} else {
		// Raw CAN frames: 8-byte standard format [ID(4)][DLC(1)][Data(DLC)]
		for len(data) >= 8 {
			id := binary.LittleEndian.Uint32(data[:4])
			dlc := data[4] & 0x0F
			frame := CANFrame{
				ID:        id,
				DLC:       dlc,
				Data:      make([]byte, dlc),
				IsFD:      cfg.FDEnabled,
				BRS:       cfg.BRS,
				Timestamp: time.Now(),
			}
			if int(dlc) <= len(data)-8 {
				copy(frame.Data, data[8:8+int(dlc)])
			}
			// Apply filter
			if cfg.FilterID != 0 && cfg.FilterMask != 0 {
				if id&cfg.FilterMask != cfg.FilterID {
					data = data[8+int(dlc):]
					continue
				}
			}
			frames = append(frames, frame)
			data = data[8+int(dlc):]
		}
	}

	return frames
}

// encodeCANFrame encodes a CAN frame into the configured protocol format.
func encodeCANFrame(frame CANFrame, protocol string) []byte {
	switch protocol {
	case "LEP":
		// LEP CAN frame: [id:4][flags:1][len:1][data:N]
		// flags: DLC in low nibble, FD=0x80, BRS=0x40, IDE=0x20.
		buf := make([]byte, 6+len(frame.Data))
		binary.LittleEndian.PutUint32(buf[:4], frame.ID)
		flags := frame.DLC & 0x0F
		if frame.IsFD {
			flags |= 0x80
		}
		if frame.BRS {
			flags |= 0x40
		}
		if frame.IDE {
			flags |= 0x20
		}
		buf[4] = flags
		buf[5] = byte(len(frame.Data))
		copy(buf[6:], frame.Data)
		return buf
	case "JSON":
		return marshalCANFrameJSON(frame)
	default:
		return frame.Data
	}
}

// marshalCANFrameJSON encodes a CAN frame as JSON.
func marshalCANFrameJSON(frame CANFrame) []byte {
	var buf []byte
	buf = append(buf, '{')
	buf = append(buf, '"')
	buf = append(buf, "id"...)
	buf = append(buf, '"', ':')
	buf = append(buf, hex.EncodeToString(binary.LittleEndian.AppendUint32(nil, frame.ID))...)
	buf = append(buf, ',')
	buf = append(buf, '"')
	buf = append(buf, "dlc"...)
	buf = append(buf, '"', ':')
	buf = append(buf, byte('0'+frame.DLC))
	buf = append(buf, ',')
	buf = append(buf, '"')
	buf = append(buf, "data"...)
	buf = append(buf, '"', ':')
	buf = append(buf, '"')
	buf = append(buf, hex.EncodeToString(frame.Data)...)
	buf = append(buf, '"')
	buf = append(buf, '}')
	return buf
}

// decodeCANFrameLinux decodes a raw CAN/CANFD frame received from an AF_CAN
// socket. Classic CAN frames are 16 bytes (struct can_frame) and CAN FD
// frames are 72 bytes (struct canfd_frame). The kernel layout is identical
// on every platform that produces SocketCAN frames, so this is shared code.
func decodeCANFrameLinux(buf []byte) CANFrame {
	if len(buf) < 16 {
		return CANFrame{Timestamp: time.Now()}
	}
	id := binary.LittleEndian.Uint32(buf[:4])
	isFD := len(buf) >= 72
	dlc := buf[4] & 0x0F
	// Classic can_frame: flags (IDE/RTR) live in the high bits of byte 4.
	// CAN FD canfd_frame: flags live in byte 5 (FD=0x01, BRS=0x02, ESI=0x04).
	flags := buf[4]
	if isFD {
		flags = buf[5]
	}
	frame := CANFrame{
		ID:        id,
		DLC:       dlc,
		IsFD:      isFD,
		BRS:       flags&0x02 != 0,
		IDE:       flags&0x04 != 0,
		Timestamp: time.Now(),
	}
	n := 8
	if isFD {
		n = 64
	}
	if int(dlc) > n {
		dlc = uint8(n)
	}
	frame.Data = make([]byte, dlc)
	copy(frame.Data, buf[8:8+int(dlc)])
	return frame
}

// CANDevice represents a physical CAN device for HIL testing.
type CANDevice struct {
	Interface string
	Baudrate  int
	Frames    []CANFrame
	mu        sync.RWMutex
}

// NewCANDevice creates a CAN device for simulation.
func NewCANDevice(iface string, baudrate int) *CANDevice {
	return &CANDevice{
		Interface: iface,
		Baudrate:  baudrate,
		Frames:    make([]CANFrame, 0),
	}
}

// InjectFrame injects a CAN frame into the device's output queue.
func (d *CANDevice) InjectFrame(frame CANFrame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Frames = append(d.Frames, frame)
}

// InjectFrames injects multiple CAN frames.
func (d *CANDevice) InjectFrames(frames []CANFrame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Frames = append(d.Frames, frames...)
}

// Available returns true if the device is ready for communication.
func (d *CANDevice) Available() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Interface != ""
}

// Name returns the device interface name.
func (d *CANDevice) Name() string {
	return d.Interface
}

// SanitizeCANInterface returns a clean interface index suffix. Both "can0"
// and "vcan1" yield the numeric tail ("0", "1") used for addressing.
func SanitizeCANInterface(name string) string {
	name = strings.TrimPrefix(name, "vcan")
	return strings.TrimPrefix(name, "can")
}
