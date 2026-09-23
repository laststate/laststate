// Package protocol is the Trace-facing re-export of the LEP codec.
// When github.com/laststate/protocol is published as a Go module, replace the
// implementation below with a thin wrapper:
//
//	import prot "github.com/laststate/protocol/lep"
//
// Until then this package is the single import path for Trace and Relay-side code.
package protocol

import "github.com/laststate/trace/internal/lep"

const (
	HeaderSize      = lep.HeaderSize
	MaxEnvelopeSize = lep.MaxEnvelopeSize
	Magic           = lep.Magic
	Version1        = lep.Version1
	Version2        = lep.Version2
	MinVersion      = lep.MinVersion
	CurrentVersion  = lep.CurrentVersion

	FlagAuthenticated = lep.FlagAuthenticated
	FlagEncrypted     = lep.FlagEncrypted
	FlagAEAD          = lep.FlagAEAD
	FlagTruncated     = lep.FlagTruncated
	FlagCompressed    = lep.FlagCompressed

	TypeCrash      = lep.TypeCrash
	TypeError      = lep.TypeError
	TypeMessage    = lep.TypeMessage
	TypeHealth     = lep.TypeHealth
	TypeReset      = lep.TypeReset
	TypeLog        = lep.TypeLog
	TypePeripheral = lep.TypePeripheral
	TypeCoredump   = lep.TypeCoredump

	TLVIdentity     = lep.TLVIdentity
	TLVTimestamp    = lep.TLVTimestamp
	TLVEvent        = lep.TLVEvent
	TLVCPU          = lep.TLVCPU
	TLVFault        = lep.TLVFault
	TLVAssert       = lep.TLVAssert
	TLVStack        = lep.TLVStack
	TLVBuildID      = lep.TLVBuildID
	TLVProjectID    = lep.TLVProjectID
	TLVReleaseID    = lep.TLVReleaseID
	TLVFirmwareHash = lep.TLVFirmwareHash
	TLVBootID       = lep.TLVBootID
	TLVAttachment   = lep.TLVAttachment
	TLVExtension    = lep.TLVExtension
)

type (
	Header          = lep.Header
	TLV             = lep.TLV
	ValidationError = lep.ValidationError
	ErrorKind       = lep.ErrorKind
)

var (
	Validate      = lep.Validate
	Payload       = lep.Payload
	ParseTLVs     = lep.ParseTLVs
	Encode        = lep.Encode
	EncodeTLVs    = lep.EncodeTLVs
	EventTypeName = lep.EventTypeName
	ArchName      = lep.ArchName
)

const (
	ErrorCorrupt     = lep.ErrorCorrupt
	ErrorUnsupported = lep.ErrorUnsupported
	ErrorTooLarge    = lep.ErrorTooLarge
)
