package analysis

import (
	"encoding/binary"
	"fmt"

	"github.com/laststate/trace/internal/decode"
)

// RTOS task awareness (FreeRTOS / Zephyr style TLV layout).
// TLV type 0x0030 = RTOS snapshot (custom extension until protocol freezes it).

const TLVRTOS uint16 = 0x0030
const TLVProbe uint16 = 0x0031

type Task struct {
	Name     string `json:"name"`
	Priority uint8  `json:"priority"`
	State    string `json:"state"`
	StackTop uint32 `json:"stack_top,omitempty"`
	StackEnd uint32 `json:"stack_end,omitempty"`
	PC       uint32 `json:"pc,omitempty"`
}

type RTOSInfo struct {
	Kind  string `json:"kind"` // freertos|zephyr|unknown
	Tasks []Task `json:"tasks,omitempty"`
}

type ProbeInfo struct {
	Serial  string `json:"serial,omitempty"`
	Backend string `json:"backend,omitempty"` // jlink|cmsis-dap|openocd
	Target  string `json:"target,omitempty"`
}

func EnrichRTOS(r *Report, d decode.Decoded) {
	for _, t := range d.TLVs {
		switch t.Type {
		case TLVRTOS:
			info := parseRTOS(t.Value)
			if len(info.Tasks) > 0 {
				r.Warnings = append(r.Warnings, fmt.Sprintf("rtos:%s tasks=%d", info.Kind, len(info.Tasks)))
				// promote current task PC if present
				for _, task := range info.Tasks {
					if task.State == "running" && task.PC != 0 {
						r.Frames = append([]Frame{{Address: uint64(task.PC), Function: task.Name}}, r.Frames...)
						r.Confidence += 0.05
					}
				}
			}
			r.RTOS = &info
		case TLVProbe:
			r.Probe = parseProbe(t.Value)
		}
	}
}

func parseRTOS(v []byte) RTOSInfo {
	info := RTOSInfo{Kind: "unknown"}
	if len(v) < 2 {
		return info
	}
	switch v[0] {
	case 1:
		info.Kind = "freertos"
	case 2:
		info.Kind = "zephyr"
	}
	// layout: kind(u8) count(u8) then records: nameLen(u8) name prio(u8) state(u8) stackTop(u32) stackEnd(u32) pc(u32)
	count := int(v[1])
	off := 2
	for i := 0; i < count && off < len(v); i++ {
		if off >= len(v) {
			break
		}
		nl := int(v[off])
		off++
		if off+nl+1+1+12 > len(v) {
			break
		}
		name := string(v[off : off+nl])
		off += nl
		prio := v[off]
		off++
		st := v[off]
		off++
		task := Task{
			Name: name, Priority: prio, State: taskState(st),
			StackTop: binary.LittleEndian.Uint32(v[off:]),
			StackEnd: binary.LittleEndian.Uint32(v[off+4:]),
			PC:       binary.LittleEndian.Uint32(v[off+8:]),
		}
		off += 12
		info.Tasks = append(info.Tasks, task)
	}
	return info
}

func taskState(s uint8) string {
	switch s {
	case 0:
		return "running"
	case 1:
		return "ready"
	case 2:
		return "blocked"
	case 3:
		return "suspended"
	case 4:
		return "deleted"
	default:
		return fmt.Sprintf("state_%d", s)
	}
}

func parseProbe(v []byte) *ProbeInfo {
	// name\0backend\0target
	parts := split0(v)
	p := &ProbeInfo{}
	if len(parts) > 0 {
		p.Serial = parts[0]
	}
	if len(parts) > 1 {
		p.Backend = parts[1]
	}
	if len(parts) > 2 {
		p.Target = parts[2]
	}
	return p
}

func split0(v []byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(v); i++ {
		if v[i] == 0 {
			out = append(out, string(v[start:i]))
			start = i + 1
		}
	}
	if start < len(v) {
		out = append(out, string(v[start:]))
	}
	return out
}
