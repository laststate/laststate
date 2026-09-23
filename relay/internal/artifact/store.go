// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package artifact stores firmware images by content hash and GNU build id.
package artifact

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Artifact struct {
	SHA256       string    `json:"sha256"`
	Path         string    `json:"path"`
	OriginalName string    `json:"original_name"`
	Size         int64     `json:"size"`
	Kind         string    `json:"kind"`
	BuildID      string    `json:"build_id,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	ReleaseID    string    `json:"release_id,omitempty"`
	FirmwareHash string    `json:"firmware_hash,omitempty"`
	Provenance   string    `json:"provenance,omitempty"`
	Architecture string    `json:"architecture,omitempty"`
	Class        string    `json:"class,omitempty"`
	Endianness   string    `json:"endianness,omitempty"`
	HasDWARF     bool      `json:"has_dwarf"`
	DebugLink    string    `json:"debug_link,omitempty"`
	AddedAt      time.Time `json:"added_at"`
}

func Add(dataDir, source string) (Artifact, error) {
	return AddTo(filepath.Join(dataDir, "artifacts"), source)
}

func AddTo(artifactDir, source string) (Artifact, error) {
	input, err := os.Open(source)
	if err != nil {
		return Artifact{}, err
	}
	defer input.Close()
	root := filepath.Join(artifactDir, "sha256")
	if err := os.MkdirAll(root, 0700); err != nil {
		return Artifact{}, err
	}
	hasher := sha256.New()
	temporary, err := os.CreateTemp(root, ".pending-artifact-*")
	if err != nil {
		return Artifact{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return Artifact{}, err
	}
	written, err := io.Copy(io.MultiWriter(temporary, hasher), input)
	if err != nil {
		temporary.Close()
		return Artifact{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Artifact{}, err
	}
	if err := temporary.Close(); err != nil {
		return Artifact{}, err
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	path := filepath.Join(root, sum)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.Rename(temporaryName, path); err != nil {
			return Artifact{}, err
		}
		if directory, err := os.Open(root); err == nil {
			_ = directory.Sync()
			_ = directory.Close()
		}
	}
	item := Artifact{
		SHA256: sum, Path: path, OriginalName: filepath.Base(source), Size: written,
		Kind: detectKind(source), AddedAt: time.Now().UTC(),
	}
	if item.Kind == "elf" {
		metadata, err := inspectELF(path)
		if err != nil {
			return Artifact{}, fmt.Errorf("inspect ELF: %w", err)
		}
		item.BuildID = metadata.BuildID
		item.Architecture = metadata.Architecture
		item.Class = metadata.Class
		item.Endianness = metadata.Endianness
		item.HasDWARF = metadata.HasDWARF
	}
	if err := writeManifest(item); err != nil {
		return Artifact{}, err
	}
	return item, nil
}

func Inspect(path string) (Artifact, error) {
	data, err := os.ReadFile(path + ".json")
	if err == nil {
		var item Artifact
		if err := json.Unmarshal(data, &item); err == nil {
			return item, nil
		}
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return Artifact{}, statErr
	}
	item := Artifact{Path: path, OriginalName: filepath.Base(path), Size: info.Size(), Kind: detectKind(path)}
	if item.Kind == "elf" {
		metadata, inspectErr := inspectELF(path)
		if inspectErr != nil {
			return Artifact{}, inspectErr
		}
		item.BuildID, item.Architecture, item.Class, item.Endianness, item.HasDWARF = metadata.BuildID, metadata.Architecture, metadata.Class, metadata.Endianness, metadata.HasDWARF
	}
	return item, nil
}

func List(dataDir string) ([]Artifact, error) {
	return ListFrom(filepath.Join(dataDir, "artifacts"))
}

func ListFrom(artifactDir string) ([]Artifact, error) {
	directory := filepath.Join(artifactDir, "sha256")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		item, err := Inspect(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if item.SHA256 == "" {
			item.SHA256 = entry.Name()
		}
		artifacts = append(artifacts, item)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].AddedAt.After(artifacts[j].AddedAt) })
	return artifacts, nil
}

func FindByBuildID(dataDir, buildID string) (Artifact, error) {
	return FindByBuildIDFrom(filepath.Join(dataDir, "artifacts"), buildID)
}

func FindByBuildIDFrom(artifactDir, buildID string) (Artifact, error) {
	items, err := ListFrom(artifactDir)
	if err != nil {
		return Artifact{}, err
	}
	buildID = strings.ToLower(strings.TrimSpace(buildID))
	for _, item := range items {
		if strings.ToLower(item.BuildID) == buildID {
			return item, nil
		}
	}
	return Artifact{}, os.ErrNotExist
}

func Verify(item Artifact) error {
	file, err := os.Open(item.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, file)
	if err != nil {
		return err
	}
	if written != item.Size {
		return fmt.Errorf("artifact size mismatch: manifest=%d actual=%d", item.Size, written)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if item.SHA256 != "" && actual != item.SHA256 {
		return fmt.Errorf("artifact checksum mismatch")
	}
	return nil
}

func writeManifest(item Artifact) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(item.Path), ".pending-manifest-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, item.Path+".json")
}

func detectKind(path string) string {
	file, err := elf.Open(path)
	if err == nil {
		file.Close()
		return "elf"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".hex", ".ihex":
		return "hex"
	case ".bin":
		return "binary"
	case ".map":
		return "map"
	default:
		return "unknown"
	}
}

type elfMetadata struct {
	BuildID, Architecture, Class, Endianness string
	HasDWARF                                 bool
}

func inspectELF(path string) (elfMetadata, error) {
	file, err := elf.Open(path)
	if err != nil {
		return elfMetadata{}, err
	}
	defer file.Close()
	metadata := elfMetadata{
		Architecture: file.Machine.String(), Class: file.Class.String(), Endianness: byteOrderName(file.ByteOrder),
		HasDWARF: file.Section(".debug_info") != nil || file.Section(".zdebug_info") != nil,
	}
	if section := file.Section(".note.gnu.build-id"); section != nil {
		data, err := section.Data()
		if err == nil {
			metadata.BuildID = parseGNUBuildID(data, file.ByteOrder)
		}
	}
	return metadata, nil
}

func parseGNUBuildID(data []byte, order binary.ByteOrder) string {
	for offset := 0; offset+12 <= len(data); {
		namesz := int(order.Uint32(data[offset : offset+4]))
		descsz := int(order.Uint32(data[offset+4 : offset+8]))
		typeID := order.Uint32(data[offset+8 : offset+12])
		offset += 12
		nameEnd := offset + namesz
		if nameEnd > len(data) {
			return ""
		}
		name := strings.TrimRight(string(data[offset:nameEnd]), "\x00")
		offset = align4(nameEnd)
		descEnd := offset + descsz
		if descEnd > len(data) {
			return ""
		}
		if name == "GNU" && typeID == 3 {
			return hex.EncodeToString(data[offset:descEnd])
		}
		offset = align4(descEnd)
	}
	return ""
}

func align4(value int) int { return (value + 3) &^ 3 }
func byteOrderName(order binary.ByteOrder) string {
	if order == binary.LittleEndian {
		return "little"
	}
	return "big"
}
