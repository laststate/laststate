// Package artifact inspects uploaded firmware images (ELF build-id).
package artifact

import (
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

type Meta struct {
	BuildID      string
	Architecture string
	Class        string
	Endianness   string
	HasDWARF     bool
}

func InspectFile(path string) (Meta, error) {
	f, err := elf.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	return inspect(f)
}

func Inspect(r io.ReaderAt) (Meta, error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	return inspect(f)
}

func inspect(f *elf.File) (Meta, error) {
	m := Meta{
		Class:        f.Class.String(),
		Endianness:   f.ByteOrder.String(),
		Architecture: archName(f.Machine),
	}
	if sec := f.Section(".debug_info"); sec != nil && sec.Size > 0 {
		m.HasDWARF = true
	}
	for _, s := range f.Sections {
		if s.Type != elf.SHT_NOTE {
			continue
		}
		data, err := s.Data()
		if err != nil {
			continue
		}
		if id := parseGNUBuildID(data, f.ByteOrder); id != "" {
			m.BuildID = id
			break
		}
	}
	if m.BuildID == "" {
		return m, fmt.Errorf("no GNU build-id note")
	}
	return m, nil
}

func archName(m elf.Machine) string {
	switch m {
	case elf.EM_ARM:
		return "cortex-m"
	case elf.EM_RISCV:
		return "riscv"
	case elf.EM_XTENSA:
		return "xtensa"
	case elf.EM_X86_64, elf.EM_386:
		return "linux"
	default:
		return strings.ToLower(m.String())
	}
}

func parseGNUBuildID(data []byte, order binary.ByteOrder) string {
	off := 0
	for off+12 <= len(data) {
		namesz := int(order.Uint32(data[off : off+4]))
		descsz := int(order.Uint32(data[off+4 : off+8]))
		typ := order.Uint32(data[off+8 : off+12])
		off += 12
		if namesz <= 0 || descsz < 0 {
			break
		}
		nameEnd := off + namesz
		if nameEnd > len(data) {
			break
		}
		name := strings.TrimRight(string(data[off:nameEnd]), "\x00")
		off = align4(nameEnd)
		descEnd := off + descsz
		if descEnd > len(data) {
			break
		}
		if typ == 3 && name == "GNU" && descsz > 0 {
			return strings.ToLower(hex.EncodeToString(data[off:descEnd]))
		}
		off = align4(descEnd)
	}
	return ""
}

func align4(n int) int {
	return (n + 3) &^ 3
}

func WriteTemp(raw []byte) (string, error) {
	f, err := os.CreateTemp("", "trace-art-*.elf")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
