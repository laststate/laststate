// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package symbolicate maps addresses to functions and source lines.
package symbolicate

import (
	"debug/dwarf"
	"debug/elf"
	"fmt"
	"path/filepath"
)

type Frame struct {
	Address  uint64 `json:"address"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

type Options struct {
	PathMap      PathMap
	Cache        *Cache
	ArtifactSHA  string
	PreferInline bool
}

// Resolve uses DWARF directly, avoiding a shell and external toolchain.
func Resolve(path string, addresses []uint64) ([]Frame, error) {
	return ResolveWithOptions(path, addresses, Options{})
}

func ResolveWithOptions(path string, addresses []uint64, opts Options) ([]Frame, error) {
	frames := make([]Frame, len(addresses))
	missing := make([]uint64, 0, len(addresses))
	missingIdx := make([]int, 0, len(addresses))
	for i, address := range addresses {
		if cached, ok := opts.Cache.Get(opts.ArtifactSHA, address); ok {
			frames[i] = cached
			frames[i].File = opts.PathMap.Rewrite(frames[i].File)
			frames[i].Function = Demangle(frames[i].Function)
			continue
		}
		frames[i] = Frame{Address: address}
		missing = append(missing, address)
		missingIdx = append(missingIdx, i)
	}
	if len(missing) == 0 {
		return frames, nil
	}

	file, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := file.DWARF()
	if err != nil {
		return nil, fmt.Errorf("load DWARF: %w", err)
	}

	for j, address := range missing {
		i := missingIdx[j]
		fn, inline := functionAt(data, address)
		frames[i].Function = Demangle(fn)
		frames[i].Inline = inline
		fileName, line, column := lineAt(data, address)
		frames[i].File = opts.PathMap.Rewrite(filepath.ToSlash(fileName))
		frames[i].Line = line
		frames[i].Column = column
		_ = opts.Cache.Put(opts.ArtifactSHA, frames[i])
	}
	return frames, nil
}

// ResolveDebugFile tries the primary path then a sibling .debug file for stripped builds.
func ResolveDebugFile(path string, addresses []uint64, opts Options) ([]Frame, []string, error) {
	var warnings []string
	frames, err := ResolveWithOptions(path, addresses, opts)
	if err == nil {
		empty := true
		for _, frame := range frames {
			if frame.Function != "" || frame.File != "" {
				empty = false
				break
			}
		}
		if !empty {
			return frames, warnings, nil
		}
		warnings = append(warnings, "primary ELF produced no symbol names; trying separate debug files")
	} else {
		warnings = append(warnings, "primary DWARF: "+err.Error())
	}
	for _, candidate := range []string{path + ".debug", path + ".dwo", stringsTrimSuffix(path, filepath.Ext(path)) + ".debug"} {
		if candidate == path {
			continue
		}
		frames, err := ResolveWithOptions(candidate, addresses, opts)
		if err == nil {
			warnings = append(warnings, "used separate debug file "+candidate)
			return frames, warnings, nil
		}
	}
	if err != nil {
		return frames, warnings, err
	}
	return frames, warnings, nil
}

func stringsTrimSuffix(s, suffix string) string {
	if len(suffix) > 0 && len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

func functionAt(data *dwarf.Data, address uint64) (string, bool) {
	reader := data.Reader()
	var bestName string
	var bestInline bool
	var bestSize = ^uint64(0)
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}
		if entry.Tag != dwarf.TagSubprogram && entry.Tag != dwarf.TagInlinedSubroutine {
			continue
		}
		ranges, err := data.Ranges(entry)
		if err != nil {
			continue
		}
		for _, r := range ranges {
			if address >= r[0] && address < r[1] {
				size := r[1] - r[0]
				name := ""
				if n, ok := entry.Val(dwarf.AttrName).(string); ok {
					name = n
				} else if n, ok := entry.Val(dwarf.AttrLinkageName).(string); ok {
					name = n
				}
				if name != "" && size <= bestSize {
					bestSize = size
					bestName = name
					bestInline = entry.Tag == dwarf.TagInlinedSubroutine
				}
			}
		}
	}
	return bestName, bestInline
}

func lineAt(data *dwarf.Data, address uint64) (string, int, int) {
	reader := data.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			return "", 0, 0
		}
		if entry.Tag != dwarf.TagCompileUnit {
			continue
		}
		lineReader, err := data.LineReader(entry)
		if err != nil || lineReader == nil {
			continue
		}
		var lineEntry dwarf.LineEntry
		if err := lineReader.SeekPC(address, &lineEntry); err == nil && lineEntry.File != nil {
			return lineEntry.File.Name, lineEntry.Line, lineEntry.Column
		}
		reader.SkipChildren()
	}
}
