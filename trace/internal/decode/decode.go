// Package decode extracts identity and event metadata from LEP TLVs.
package decode

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/laststate/trace/internal/lep"
)

const (
	TLVIdentity     uint16 = lep.TLVIdentity
	TLVReset        uint16 = lep.TLVReset
	TLVEvent        uint16 = lep.TLVEvent
	TLVCPU          uint16 = lep.TLVCPU
	TLVFault        uint16 = lep.TLVFault
	TLVBreadcrumb   uint16 = lep.TLVBreadcrumb
	TLVLog          uint16 = lep.TLVLog
	TLVAssert       uint16 = lep.TLVAssert
	TLVStack        uint16 = lep.TLVStack
	TLVBuildID      uint16 = lep.TLVBuildID
	TLVProjectID    uint16 = lep.TLVProjectID
	TLVReleaseID    uint16 = lep.TLVReleaseID
	TLVFirmwareHash uint16 = lep.TLVFirmwareHash
	TLVBootID       uint16 = lep.TLVBootID
	TLVAttachment   uint16 = lep.TLVAttachment
	TLVExtension    uint16 = lep.TLVExtension
)

type Identity struct {
	DeviceID         string `json:"device_id,omitempty"`
	Product          string `json:"product,omitempty"`
	HardwareRevision string `json:"hardware_revision,omitempty"`
	FirmwareVersion  string `json:"firmware_version,omitempty"`
	BuildID          string `json:"build_id,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	ReleaseID        string `json:"release_id,omitempty"`
	FirmwareHash     string `json:"firmware_hash,omitempty"`
	GitCommit        string `json:"git_commit,omitempty"`
	BootID           string `json:"boot_id,omitempty"`
}

type Attachment struct {
	Name string `json:"name,omitempty"`
	MIME string `json:"mime,omitempty"`
	Size int    `json:"size"`
	// Content is base64-safe raw; large blobs truncated in JSON path
	Content []byte `json:"-"`
}

type EventMeta struct {
	Priority     uint8  `json:"priority"`
	Severity     uint8  `json:"severity"`
	CaptureLevel uint8  `json:"capture_level"`
	Code         uint32 `json:"code"`
	Fingerprint  uint32 `json:"fingerprint"`
	RepeatCount  uint32 `json:"repeat_count"`
}

type Breadcrumb struct {
	TimestampMS  uint32 `json:"timestamp,omitempty"`
	MessageID    uint16 `json:"message_id,omitempty"`
	Severity     uint8  `json:"severity,omitempty"`
	CategoryHash uint32 `json:"category_hash,omitempty"`
	MessageHash  uint32 `json:"message_hash,omitempty"`
	Category     string `json:"category,omitempty"`
	Message      string `json:"message,omitempty"`
	Type         string `json:"type,omitempty"`
}

type Decoded struct {
	Header      lep.Header   `json:"header"`
	Identity    Identity     `json:"identity"`
	Event       *EventMeta   `json:"event,omitempty"`
	Assert      string       `json:"assert,omitempty"`
	PC          uint32       `json:"pc,omitempty"`
	LR          uint32       `json:"lr,omitempty"`
	SP          uint32       `json:"sp,omitempty"`
	BootID      string       `json:"boot_id,omitempty"`
	TimestampMS uint64       `json:"timestamp_ms,omitempty"`
	Breadcrumbs []Breadcrumb `json:"breadcrumbs,omitempty"`
	LogLines    []string     `json:"logs,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Extensions  []lep.TLV    `json:"extensions,omitempty"`
	Compressed  bool         `json:"compressed,omitempty"`
	Encrypted   bool         `json:"encrypted,omitempty"`
	TLVs        []lep.TLV    `json:"-"`
}

func Envelope(raw []byte) (Decoded, error) {
	h, err := lep.Validate(raw)
	if err != nil {
		return Decoded{}, err
	}
	out := Decoded{
		Header:     h,
		Compressed: h.Flags&lep.FlagCompressed != 0,
		Encrypted:  h.Flags&lep.FlagEncrypted != 0,
	}
	payload, err := lep.Payload(raw)
	if err != nil {
		// still return header for unsupported encrypted/compressed envelopes
		return out, nil
	}
	tlvs, err := lep.ParseTLVs(payload)
	if err != nil {
		return out, err
	}
	out.TLVs = tlvs
	for _, t := range tlvs {
		switch t.Type {
		case TLVIdentity:
			out.Identity = mergeIdentity(out.Identity, parseIdentity(t.Value))
		case TLVEvent:
			if m := parseEvent(t.Value); m != nil {
				out.Event = m
			}
		case TLVCPU:
			pc, lr, sp := parseCPU(t.Value, h.Architecture)
			out.PC, out.LR, out.SP = pc, lr, sp
		case TLVReset:
			// RESET is 20 bytes; timestamp_ms at offset 13 (u32) per protocol registry
			if len(t.Value) >= 17 {
				out.TimestampMS = uint64(binary.LittleEndian.Uint32(t.Value[13:17]))
			}
		case TLVAssert:
			out.Assert = parseAssertMessage(t.Value)
		case TLVBreadcrumb:
			out.Breadcrumbs = append(out.Breadcrumbs, parseBreadcrumb(t.Value))
		case TLVLog:
			if msg := strings.TrimSpace(string(t.Value)); msg != "" {
				out.LogLines = append(out.LogLines, msg)
				// also as breadcrumb for replay UI
				out.Breadcrumbs = append(out.Breadcrumbs, Breadcrumb{
					Type: "log", Message: msg, Category: "log",
				})
			}
		case TLVBuildID:
			out.Identity.BuildID = asID(t.Value)
		case TLVProjectID:
			out.Identity.ProjectID = string(t.Value)
		case TLVReleaseID:
			out.Identity.ReleaseID = string(t.Value)
		case TLVFirmwareHash:
			out.Identity.FirmwareHash = strings.ToLower(hex.EncodeToString(t.Value))
		case TLVBootID:
			out.BootID = asID(t.Value)
			out.Identity.BootID = out.BootID
		case TLVAttachment:
			out.Attachments = append(out.Attachments, parseAttachment(t.Value))
		case TLVExtension:
			out.Extensions = append(out.Extensions, t)
		}
	}
	return out, nil
}

// parseBreadcrumb matches Latch put_breadcrumbs layout:
// at_ms u32 | message_id u16 | severity u8 | category_hash u32 | message_hash u32
// optional nested strings field_id 1=category, 2=message
func parseBreadcrumb(v []byte) Breadcrumb {
	b := Breadcrumb{Type: "breadcrumb"}
	if len(v) < 15 {
		if len(v) > 0 {
			b.Message = string(v)
		}
		return b
	}
	b.TimestampMS = binary.LittleEndian.Uint32(v[0:4])
	b.MessageID = binary.LittleEndian.Uint16(v[4:6])
	b.Severity = v[6]
	b.CategoryHash = binary.LittleEndian.Uint32(v[7:11])
	b.MessageHash = binary.LittleEndian.Uint32(v[11:15])
	// optional string fields after fixed header (+ value_count u8 + kvs)
	off := 15
	if off < len(v) {
		// value_count may follow strings; scan nested field_id|len|utf8
		for off+2 <= len(v) {
			fid := v[off]
			if fid == 0 || fid > 15 {
				break
			}
			sl := int(v[off+1])
			off += 2
			if off+sl > len(v) {
				break
			}
			s := string(v[off : off+sl])
			off += sl
			switch fid {
			case 1:
				b.Category = s
			case 2:
				b.Message = s
			}
		}
	}
	if b.Message == "" && b.MessageHash != 0 {
		b.Message = fmt.Sprintf("msg#%d", b.MessageID)
	}
	return b
}

func parseAttachment(v []byte) Attachment {
	// layout: name_len(u8) name mime_len(u8) mime rest=content
	a := Attachment{Size: len(v)}
	if len(v) < 2 {
		a.Content = v
		return a
	}
	nl := int(v[0])
	if 1+nl >= len(v) {
		a.Content = v
		return a
	}
	a.Name = string(v[1 : 1+nl])
	off := 1 + nl
	if off >= len(v) {
		return a
	}
	ml := int(v[off])
	off++
	if off+ml <= len(v) {
		a.MIME = string(v[off : off+ml])
		off += ml
	}
	if off < len(v) {
		a.Content = append([]byte(nil), v[off:]...)
		a.Size = len(a.Content)
	}
	return a
}

func SeverityName(sev uint8) string {
	switch sev {
	case 0:
		return "debug"
	case 1:
		return "info"
	case 2:
		return "warning"
	case 3:
		return "error"
	case 4:
		return "fatal"
	default:
		return "error"
	}
}

func parseIdentity(value []byte) Identity {
	var id Identity
	for off := 0; off+2 <= len(value); {
		fid := value[off]
		n := int(value[off+1])
		off += 2
		if off+n > len(value) {
			break
		}
		s := string(value[off : off+n])
		off += n
		switch fid {
		case 1:
			id.ProjectID = s
		case 2:
			id.DeviceID = s
		case 3:
			id.Product = s
		case 4:
			id.HardwareRevision = s
		case 7:
			id.FirmwareVersion = s
		case 8:
			id.BuildID = strings.ToLower(s)
		case 10:
			id.GitCommit = s
		}
	}
	return id
}

func mergeIdentity(a, b Identity) Identity {
	if b.DeviceID != "" {
		a.DeviceID = b.DeviceID
	}
	if b.Product != "" {
		a.Product = b.Product
	}
	if b.HardwareRevision != "" {
		a.HardwareRevision = b.HardwareRevision
	}
	if b.FirmwareVersion != "" {
		a.FirmwareVersion = b.FirmwareVersion
	}
	if b.BuildID != "" {
		a.BuildID = b.BuildID
	}
	if b.ProjectID != "" {
		a.ProjectID = b.ProjectID
	}
	if b.ReleaseID != "" {
		a.ReleaseID = b.ReleaseID
	}
	if b.FirmwareHash != "" {
		a.FirmwareHash = b.FirmwareHash
	}
	if b.GitCommit != "" {
		a.GitCommit = b.GitCommit
	}
	return a
}

func parseEvent(value []byte) *EventMeta {
	if len(value) < 28 {
		return nil
	}
	return &EventMeta{
		Priority:     value[0],
		Severity:     value[1],
		CaptureLevel: value[2],
		Code:         binary.LittleEndian.Uint32(value[7:11]),
		Fingerprint:  binary.LittleEndian.Uint32(value[15:19]),
		RepeatCount:  binary.LittleEndian.Uint32(value[19:23]),
	}
}

func parseCPU(value []byte, arch uint8) (pc, lr, sp uint32) {
	if len(value) < 2 {
		return
	}
	cpuArch := value[0]
	if cpuArch == 0 {
		cpuArch = arch
	}
	// Latch multi-arch container: specials start after 2 + 128 register bytes = 130
	if len(value) >= 138 {
		// specials: lr@130, pc@134; msp often at 126 within register block end
		lr = binary.LittleEndian.Uint32(value[130:134])
		pc = binary.LittleEndian.Uint32(value[134:138])
		if len(value) >= 142 {
			// msp is specials[3] = offset 130+12 = 142? specials order: lr,pc,xpsr,msp,...
			// lr@130, pc@134, xpsr@138, msp@142
			if len(value) >= 146 {
				sp = binary.LittleEndian.Uint32(value[142:146])
			}
		}
		return
	}
	if len(value) >= 12 {
		pc = binary.LittleEndian.Uint32(value[4:8])
		lr = binary.LittleEndian.Uint32(value[8:12])
	}
	return
}

func parseAssertMessage(value []byte) string {
	// line u32 | expr_hash u32 | file_hash u32 | optional nested strings
	if len(value) < 12 {
		return ""
	}
	off := 12
	var parts []string
	for off+2 <= len(value) {
		fid := value[off]
		n := int(value[off+1])
		off += 2
		if off+n > len(value) {
			break
		}
		s := string(value[off : off+n])
		off += n
		if fid >= 1 && fid <= 3 && s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func asID(value []byte) string {
	if printable(value) {
		return strings.ToLower(strings.TrimSpace(string(value)))
	}
	return strings.ToLower(hex.EncodeToString(value))
}

func printable(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	for _, b := range value {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}
