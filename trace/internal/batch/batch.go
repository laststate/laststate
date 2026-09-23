// Package batch implements Relay binary batch (LSBT) framing.
package batch

import (
	"encoding/binary"
	"fmt"
)

const Magic = "LSBT"
const Version = 1
const FlagZstd uint8 = 1 << 0

// Safety limits for untrusted allocations.
const (
	DefaultMaxEvents  = 100
	DefaultMaxEventID = 512
	DefaultMaxPayload = 4 << 20
	DefaultMaxTotal   = 64 << 20
)

type Event struct {
	EventID string
	Payload []byte
}

type Limits struct {
	MaxEvents  uint32
	MaxEventID int
	MaxPayload int
	MaxTotal   int
}

func DefaultLimits() Limits {
	return Limits{
		MaxEvents:  DefaultMaxEvents,
		MaxEventID: DefaultMaxEventID,
		MaxPayload: DefaultMaxPayload,
		MaxTotal:   DefaultMaxTotal,
	}
}

func Decode(data []byte) ([]Event, error) {
	return DecodeLimited(data, DefaultLimits())
}

func DecodeLimited(data []byte, lim Limits) ([]Event, error) {
	if lim.MaxEvents == 0 {
		lim.MaxEvents = DefaultMaxEvents
	}
	if lim.MaxEventID == 0 {
		lim.MaxEventID = DefaultMaxEventID
	}
	if lim.MaxPayload == 0 {
		lim.MaxPayload = DefaultMaxPayload
	}
	if lim.MaxTotal == 0 {
		lim.MaxTotal = DefaultMaxTotal
	}
	if len(data) > lim.MaxTotal {
		return nil, fmt.Errorf("batch exceeds max total size")
	}
	if len(data) < 12 || string(data[0:4]) != Magic {
		return nil, fmt.Errorf("invalid binary batch magic")
	}
	if data[4] != Version {
		return nil, fmt.Errorf("unsupported binary batch version %d", data[4])
	}
	if data[5]&FlagZstd != 0 {
		return nil, fmt.Errorf("compressed binary batch not supported yet")
	}
	count := binary.LittleEndian.Uint32(data[8:12])
	if count > lim.MaxEvents {
		return nil, fmt.Errorf("batch event count %d exceeds max %d", count, lim.MaxEvents)
	}
	// Cap preallocation to avoid OOM from malicious count even if we re-check bounds.
	capN := count
	if capN > lim.MaxEvents {
		capN = lim.MaxEvents
	}
	off := 12
	out := make([]Event, 0, capN)
	totalPayload := 0
	for i := uint32(0); i < count; i++ {
		if off+2 > len(data) {
			return nil, fmt.Errorf("truncated event id length")
		}
		idLen := int(binary.LittleEndian.Uint16(data[off : off+2]))
		off += 2
		if idLen > lim.MaxEventID {
			return nil, fmt.Errorf("event id too long")
		}
		if off+idLen+4 > len(data) {
			return nil, fmt.Errorf("truncated event id")
		}
		id := string(data[off : off+idLen])
		off += idLen
		n := int(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
		if n < 0 || n > lim.MaxPayload {
			return nil, fmt.Errorf("event payload size out of bounds")
		}
		if off+n > len(data) {
			return nil, fmt.Errorf("truncated event payload")
		}
		totalPayload += n
		if totalPayload > lim.MaxTotal {
			return nil, fmt.Errorf("batch payload total exceeds limit")
		}
		payload := append([]byte(nil), data[off:off+n]...)
		off += n
		out = append(out, Event{EventID: id, Payload: payload})
	}
	return out, nil
}

func Encode(events []Event) ([]byte, error) {
	if len(events) > DefaultMaxEvents {
		return nil, fmt.Errorf("too many events")
	}
	size := 12
	for _, e := range events {
		if len(e.EventID) > 0xffff || len(e.EventID) > DefaultMaxEventID {
			return nil, fmt.Errorf("event id too long")
		}
		if len(e.Payload) > DefaultMaxPayload {
			return nil, fmt.Errorf("payload too large")
		}
		size += 2 + len(e.EventID) + 4 + len(e.Payload)
	}
	if size > DefaultMaxTotal {
		return nil, fmt.Errorf("batch too large")
	}
	out := make([]byte, size)
	copy(out[0:4], Magic)
	out[4] = Version
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(events)))
	off := 12
	for _, e := range events {
		binary.LittleEndian.PutUint16(out[off:off+2], uint16(len(e.EventID)))
		off += 2
		copy(out[off:], e.EventID)
		off += len(e.EventID)
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(len(e.Payload)))
		off += 4
		copy(out[off:], e.Payload)
		off += len(e.Payload)
	}
	return out, nil
}
