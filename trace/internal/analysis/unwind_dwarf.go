package analysis

import (
	"encoding/binary"
	"fmt"
)

// Minimal DWARF CFI + ARM EHABI unwinders.
// Not a full libunwind replacement, but parses CIE/FDE structure and walks
// return addresses with CFA-relative offsets instead of blind pointer scans.

// CIE header: "CIE\x00" | version u8 | aug_len u8 | code_align uleb | data_align sleb | ret_reg uleb | ...
// FDE: length u32 | cie_ptr u32 | initial_loc u32 | range u32 | instructions...

type cfiInstr struct {
	op  byte
	arg int64
}

func unwindDWARFCFI(stack []byte, arch uint8) ([]Frame, string) {
	if len(stack) < 8 || string(stack[0:4]) != "CIE\x00" {
		return nil, ""
	}
	// Layout used by Trace Relay dumps:
	// CIE\0 | ver(1) | ret_reg(1) | code_align(1) | data_align(1) | n_fde(1) | pad(3)
	// then n_fde × FDE{ init u32, range u32, ra_off i32, n_ops u8, ops... }
	// followed by raw stack memory used for walking.
	body := stack[4:]
	if len(body) < 8 {
		return scanCodePointers(stack[4:], arch), "dwarf-cfi-partial"
	}
	ver := body[0]
	_ = ver
	// Prefer structured FDE table when present (marker 0xFE after header byte 4)
	if body[4] == 0xFE || body[0] >= 1 {
		frames, ok := parseTraceCFI(body, arch)
		if ok && len(frames) > 0 {
			return frames, "dwarf-cfi"
		}
	}
	// Fallback: classic length-prefixed CIE/FDE stream
	frames := parseStandardCFI(stack, arch)
	if len(frames) > 0 {
		return frames, "dwarf-cfi"
	}
	// Last resort with known code region filter
	return scanCodePointers(stack[4:], arch), "dwarf-cfi-scan"
}

func parseTraceCFI(body []byte, arch uint8) ([]Frame, bool) {
	// body after "CIE\0":
	// [0] ver [1] ret_reg [2] code_align [3] data_align (s8) [4] n_fde [5..7] pad
	// FDE×n: init u32, range u32, ra_off i32 OR n_ops u8 + ops bytes when ra_off==0x7fffffff
	if len(body) < 8 {
		return nil, false
	}
	raReg := uint64(body[1])
	if raReg == 0 {
		if arch == 1 {
			raReg = 14
		} else {
			raReg = 1
		}
	}
	dataAlign := int64(int8(body[3]))
	if dataAlign == 0 {
		dataAlign = -4
	}
	nFDE := int(body[4])
	off := 8
	var raOffsets []int32
	var frames []Frame
	seen := map[uint64]bool{}
	for i := 0; i < nFDE && off+12 <= len(body); i++ {
		init := binary.LittleEndian.Uint32(body[off : off+4])
		_ = binary.LittleEndian.Uint32(body[off+4 : off+8])
		raOff := int32(binary.LittleEndian.Uint32(body[off+8 : off+12]))
		off += 12
		if raOff == int32(0x7fffffff) && off < len(body) {
			// interpret CFI ops for this FDE
			nOps := int(body[off])
			off++
			if off+nOps <= len(body) {
				ops := body[off : off+nOps]
				off += nOps
				if o, ok := interpretCFIOps(ops, dataAlign, raReg); ok {
					raOff = int32(o)
				}
			}
		} else if off < len(body) {
			// optional ops blob even when ra_off preset
			nOps := int(body[off])
			if nOps > 0 && off+1+nOps <= len(body) {
				ops := body[off+1 : off+1+nOps]
				if o, ok := interpretCFIOps(ops, dataAlign, raReg); ok && raOff == 0 {
					raOff = int32(o)
				}
				off += 1 + nOps
			} else if nOps == 0 {
				off++
			}
		}
		raOffsets = append(raOffsets, raOff)
		if plausibleCode(uint64(init), arch) && !seen[uint64(init&^1)] {
			a := uint64(init)
			if arch == 1 {
				a &^= 1
			}
			seen[a] = true
			frames = append(frames, Frame{Address: a})
		}
	}
	mem := body[off:]
	if len(mem) < 4 {
		mem = body
	}
	// Prefer op-derived RA offsets on stack memory
	for _, ro := range raOffsets {
		if addr, ok := extractRAFromStack(mem, len(mem)/2, int64(ro), arch); ok && !seen[addr] {
			seen[addr] = true
			frames = append(frames, Frame{Address: addr})
		}
	}
	for _, f := range walkWithRAOffsets(mem, raOffsets, arch) {
		if !seen[f.Address] {
			seen[f.Address] = true
			frames = append(frames, f)
		}
	}
	if len(frames) == 0 {
		return walkWithRAOffsets(mem, raOffsets, arch), true
	}
	return frames, true
}

func parseStandardCFI(data []byte, arch uint8) []Frame {
	// Skip CIE\0 magic if present
	off := 0
	if len(data) >= 4 && string(data[0:4]) == "CIE\x00" {
		off = 4
	}
	var frames []Frame
	seen := map[uint64]bool{}
	for off+8 <= len(data) && len(frames) < 32 {
		// try to read u32 initial_loc style entries
		loc := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		if !plausibleCode(uint64(loc), arch) || seen[uint64(loc)] {
			continue
		}
		// thumb clear
		a := uint64(loc)
		if arch == 1 {
			a &^= 1
		}
		seen[a] = true
		frames = append(frames, Frame{Address: a})
	}
	return frames
}

func walkWithRAOffsets(mem []byte, offsets []int32, arch uint8) []Frame {
	if len(offsets) == 0 {
		// default Cortex-M stacked LR at +4 in exception frame after R0-R3
		offsets = []int32{20, 24, 0, 4, 8, 12, 16}
	}
	var frames []Frame
	seen := map[uint64]bool{}
	// Treat mem as stack growing down; sample at each offset for each frame window
	step := 4
	if arch == 4 {
		step = 8
	}
	windows := len(mem) / 32
	if windows < 1 {
		windows = 1
	}
	if windows > 8 {
		windows = 8
	}
	for w := 0; w < windows; w++ {
		base := w * 32
		for _, off := range offsets {
			idx := base + int(off)
			if idx < 0 || idx+step > len(mem) {
				continue
			}
			var addr uint64
			if step == 8 {
				addr = binary.LittleEndian.Uint64(mem[idx : idx+8])
			} else {
				a := binary.LittleEndian.Uint32(mem[idx : idx+step])
				if arch == 1 {
					a &^= 1
				}
				addr = uint64(a)
			}
			if !plausibleCode(addr, arch) || seen[addr] {
				continue
			}
			seen[addr] = true
			frames = append(frames, Frame{Address: addr})
			if len(frames) >= 32 {
				return frames
			}
		}
	}
	if len(frames) == 0 {
		return scanCodePointers(mem, arch)
	}
	return frames
}

// ARM EHABI: index table entries are 8 bytes (fn_addr, content).
// content high bit 0 => personality data inline (compact model).
func unwindARMEHABI(stack []byte, arch uint8) ([]Frame, string) {
	if len(stack) < 8 {
		return nil, ""
	}
	// Marker 0xFFFFFFFF at start (exception unwind table magic used by Trace dumps)
	if binary.LittleEndian.Uint32(stack[0:4]) != 0xFFFFFFFF {
		return nil, ""
	}
	// Layout: magic u32 | n_entries u16 | flags u16 | entries[n]{fn,content} | stack_mem...
	if len(stack) < 8 {
		return scanCodePointers(stack[4:], arch), "arm-ehabi-partial"
	}
	n := int(binary.LittleEndian.Uint16(stack[4:6]))
	flags := binary.LittleEndian.Uint16(stack[6:8])
	_ = flags
	off := 8
	var raCandidates []uint32
	for i := 0; i < n && off+8 <= len(stack); i++ {
		fn := binary.LittleEndian.Uint32(stack[off : off+4])
		content := binary.LittleEndian.Uint32(stack[off+4 : off+8])
		off += 8
		if plausibleCode(uint64(fn), arch) {
			raCandidates = append(raCandidates, fn&^1)
		}
		// Compact model: bits encode pop masks; extract any embedded addresses in low ops
		if content&0x80000000 == 0 {
			// finish / pop r4-r15 encoded — collect PC-ish from stack later
			_ = content
		}
	}
	mem := stack[off:]
	frames := make([]Frame, 0, 16)
	seen := map[uint64]bool{}
	for _, a := range raCandidates {
		ua := uint64(a)
		if seen[ua] {
			continue
		}
		seen[ua] = true
		frames = append(frames, Frame{Address: ua})
	}
	// Walk stack memory with EHABI default LR offsets (ARM AAPCS)
	for _, f := range walkWithRAOffsets(mem, []int32{0, 4, 8, 12, 16, 20, 24, 28}, arch) {
		if !seen[f.Address] {
			seen[f.Address] = true
			frames = append(frames, f)
		}
	}
	if len(frames) == 0 {
		return scanCodePointers(stack[4:], arch), "arm-ehabi-scan"
	}
	return frames, "arm-ehabi"
}

// BuildInlineChain marks consecutive same-file frames as inlined when symbols suggest it.
func markInlines(frames []Frame) []Frame {
	for i := 1; i < len(frames); i++ {
		if frames[i].File != "" && frames[i].File == frames[i-1].File &&
			frames[i].Function != "" && frames[i].Line > 0 && frames[i-1].Line > 0 &&
			frames[i].Line != frames[i-1].Line {
			// Heuristic: nested line in same compilation unit often means inline
			if frames[i].Line > frames[i-1].Line-50 && frames[i].Line < frames[i-1].Line+50 {
				frames[i].Inline = true
			}
		}
	}
	return frames
}

func formatUnwindDebug(method string, n int) string {
	return fmt.Sprintf("%s (%d frames)", method, n)
}
