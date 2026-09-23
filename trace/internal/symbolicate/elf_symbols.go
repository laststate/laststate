package symbolicate

import (
	"debug/elf"
	"fmt"
	"sort"
)

// ResolveELF uses the ELF symbol table as a local fallback when llvm-symbolizer
// is not installed. Resolves each address to the nearest enclosing FUNC symbol.
func ResolveELF(artifactPath string, addresses []uint64) ([]Frame, error) {
	f, err := elf.Open(artifactPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type sym struct {
		name string
		addr uint64
		size uint64
	}
	var funcs []sym
	add := func(list []elf.Symbol, err error) {
		if err != nil {
			return
		}
		for _, s := range list {
			if elf.ST_TYPE(s.Info) != elf.STT_FUNC || s.Value == 0 {
				continue
			}
			size := s.Size
			if size == 0 {
				size = 1
			}
			funcs = append(funcs, sym{name: s.Name, addr: s.Value, size: size})
		}
	}
	add(f.Symbols())
	add(f.DynamicSymbols())
	if len(funcs) == 0 {
		return nil, fmt.Errorf("no FUNC symbols in ELF")
	}
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].addr < funcs[j].addr })

	out := make([]Frame, len(addresses))
	for i, a := range addresses {
		out[i] = Frame{Address: a}
		// binary search largest addr <= a
		idx := sort.Search(len(funcs), func(j int) bool { return funcs[j].addr > a }) - 1
		if idx < 0 {
			continue
		}
		// walk back for enclosing symbol
		for j := idx; j >= 0; j-- {
			s := funcs[j]
			if a >= s.addr && a < s.addr+s.size {
				out[i].Function = s.name
				break
			}
			// if size was guessed, allow nearest previous within 64KiB
			if a >= s.addr && a-s.addr < 0x10000 {
				out[i].Function = s.name
				break
			}
		}
	}
	return out, nil
}
