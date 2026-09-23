// LEP codec — TypeScript port of the Go LEP protocol implementation
// (github.com/laststate/trace/internal/lep, aligned with laststate/protocol).
// Decodes LEP (LastState Event Protocol) envelopes into structured data.
// Decoders accept wire versions 1 and 2; encoders emit the current version (2).
//
// LEP envelope format (v1/v2):
//   - 24-byte little-endian header:
//     - Magic: 4 bytes "LSTP"
//     - Version: u8 (1 or 2)
//     - Event type: u8
//     - Architecture: u8
//     - Flags: u8 (AUTH=0x01, ENC=0x02, AEAD=0x04, TRUNCATED=0x08, COMPRESSED=0x10)
//     - Sequence: u32 LE
//     - Event ID: u32 LE
//     - Payload length: u32 LE
//     - Header CRC-32/IEEE: u32 LE
//   - Payload: TLV (Type u16 + Length u16 + Value) records
//   - Payload CRC-32/IEEE: u32 LE
//
// TLV Types (laststate/protocol registry):
//   0x01 Identity | 0x02 Reset | 0x03 Event | 0x04 CPU | 0x05 Fault
//   0x06 Breadcrumb | 0x07 Metric | 0x08 Power | 0x09 Health | 0x0A Assert
//   0x0B Peripheral | 0x0C Log | 0x0D Memory | 0x0E Stack | 0x0F Heap
//   0x10 BuildID | 0x11 ProjectID | 0x12 ReleaseID | 0x13 FirmwareHash
//   0x14 BootID | 0x20 Attachment | 0xF0 Extension

export interface LEPHeader {
  magic: string
  version: number
  eventType: number
  architecture: number
  flags: number
  sequence: number
  eventId: number
  payloadLength: number
  crc: number
  isValid: boolean
}

export type Bytes = ArrayBuffer | SharedArrayBuffer

export interface TLVRecord {
  type: number
  typeName: string
  length: number
  value: Bytes
  hex: string
}

export interface LEPEnvelope {
  header: LEPHeader
  tlvs: TLVRecord[]
  raw: Bytes
  valid: boolean
  error?: string
}

export interface CrashInfo {
  exceptionType: number
  exceptionTypeName: string
  pc: number
  sp: number
  lr: number
  r0: number
  r1: number
  r2: number
  r3: number
  xPSR: number
  faultStatus: number
  faultAddress: number
  stackTrace: number[]
}

// Version constants (matching internal/lep).
export const VERSION_1 = 1
export const VERSION_2 = 2
export const CURRENT_VERSION = VERSION_2
export const HEADER_SIZE = 24
export const MAX_ENVELOPE_SIZE = 4 << 20

// Flag bits
export const FLAG_AUTH = 0x01
export const FLAG_ENC = 0x02
export const FLAG_AEAD = 0x04
export const FLAG_TRUNCATED = 0x08
export const FLAG_COMPRESSED = 0x10

// TLV types (protocol registry)
export const TLV_IDENTITY = 0x01
export const TLV_RESET = 0x02
export const TLV_EVENT = 0x03
export const TLV_CPU = 0x04
export const TLV_FAULT = 0x05
export const TLV_BREADCRUMB = 0x06
export const TLV_METRIC = 0x07
export const TLV_POWER = 0x08
export const TLV_HEALTH = 0x09
export const TLV_ASSERT = 0x0A
export const TLV_PERIPHERAL = 0x0B
export const TLV_LOG = 0x0C
export const TLV_MEMORY = 0x0D
export const TLV_STACK = 0x0E
export const TLV_HEAP = 0x0F
export const TLV_BUILD_ID = 0x10
export const TLV_PROJECT_ID = 0x11
export const TLV_RELEASE_ID = 0x12
export const TLV_FIRMWARE_HASH = 0x13
export const TLV_BOOT_ID = 0x14
export const TLV_ATTACHMENT = 0x20
export const TLV_EXTENSION = 0xF0
// Alias used by the crash explorer: fault records carry crash context.
export const TLV_CRASH = TLV_FAULT
export const TLV_ANOMALY = TLV_FAULT

const TLV_TYPE_NAMES: Record<number, string> = {
  [TLV_IDENTITY]: 'Identity',
  [TLV_RESET]: 'Reset',
  [TLV_EVENT]: 'Event',
  [TLV_CPU]: 'CPU',
  [TLV_FAULT]: 'Fault',
  [TLV_BREADCRUMB]: 'Breadcrumb',
  [TLV_METRIC]: 'Metric',
  [TLV_POWER]: 'Power',
  [TLV_HEALTH]: 'Health',
  [TLV_ASSERT]: 'Assert',
  [TLV_PERIPHERAL]: 'Peripheral',
  [TLV_LOG]: 'Log',
  [TLV_MEMORY]: 'Memory',
  [TLV_STACK]: 'Stack',
  [TLV_HEAP]: 'Heap',
  [TLV_BUILD_ID]: 'Build ID',
  [TLV_PROJECT_ID]: 'Project ID',
  [TLV_RELEASE_ID]: 'Release ID',
  [TLV_FIRMWARE_HASH]: 'Firmware Hash',
  [TLV_BOOT_ID]: 'Boot ID',
  [TLV_ATTACHMENT]: 'Attachment',
  [TLV_EXTENSION]: 'Extension',
}

function getTLVTypeName(type: number): string {
  return TLV_TYPE_NAMES[type] || `Unknown (0x${type.toString(16)})`
}

// CRC-32/IEEE implementation
function crc32(data: Bytes): number {
  const buf = new Uint8Array(data)
  let crc = 0xffffffff
  const table = new Uint32Array(256)

  // Build CRC table
  for (let i = 0; i < 256; i++) {
    let c = i
    for (let j = 0; j < 8; j++) {
      c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1)
    }
    table[i] = c
  }

  // Compute CRC
  for (let i = 0; i < buf.length; i++) {
    crc = table[(crc ^ buf[i]) & 0xff] ^ (crc >>> 8)
  }

  return (crc ^ 0xffffffff) >>> 0
}

// Read u8
function readU8(buf: DataView, offset: number): number {
  return buf.getUint8(offset)
}

// Read u16 little-endian
function readU16LE(buf: DataView, offset: number): number {
  return buf.getUint16(offset, true)
}

// Read u32 little-endian
function readU32LE(buf: DataView, offset: number): number {
  return buf.getUint32(offset, true)
}

function emptyHeader(): LEPHeader {
  return { magic: '', version: 0, eventType: 0, architecture: 0, flags: 0, sequence: 0, eventId: 0, payloadLength: 0, crc: 0, isValid: false }
}

// Parse header (24 bytes)
function parseHeader(data: DataView, full: Uint8Array): LEPHeader {
  const magic = String.fromCharCode(
    data.getUint8(0), data.getUint8(1),
    data.getUint8(2), data.getUint8(3)
  )
  const version = readU8(data, 4)
  const eventType = readU8(data, 5)
  const architecture = readU8(data, 6)
  const flags = readU8(data, 7)
  const sequence = readU32LE(data, 8)
  const eventId = readU32LE(data, 12)
  const payloadLength = readU32LE(data, 16)
  const crc = readU32LE(data, 20)

  const headerCrcOk = crc === crc32(full.buffer.slice(0, 20))

  return {
    magic,
    version,
    eventType,
    architecture,
    flags,
    sequence,
    eventId,
    payloadLength,
    crc,
    isValid: headerCrcOk && magic === 'LSTP' && version >= VERSION_1 && version <= CURRENT_VERSION,
  }
}

// Parse TLV records from payload
function parseTLVs(data: Uint8Array, offset: number, length: number): TLVRecord[] {
  const tlvs: TLVRecord[] = []
  let pos = offset
  const end = offset + length

  while (pos + 4 <= end) {
    const type = readU16LE(new DataView(data.buffer, data.byteOffset, data.byteLength), pos)
    const len = readU16LE(new DataView(data.buffer, data.byteOffset, data.byteLength), pos + 2)

    if (pos + 4 + len > end) {
      // Truncated TLV
      tlvs.push({
        type,
        typeName: getTLVTypeName(type),
        length: len,
        value: new ArrayBuffer(0),
        hex: '',
      })
      break
    }

    const value = data.slice(pos + 4, pos + 4 + len)
    const hex = Array.from(value).map(b => b.toString(16).padStart(2, '0')).join(' ')

    tlvs.push({
      type,
      typeName: getTLVTypeName(type),
      length: len,
      value: value.buffer,
      hex,
    })

    pos += 4 + len
  }

  return tlvs
}

// Parse crash info from fault TLV value
export function parseCrashInfo(tlv: TLVRecord): CrashInfo | null {
  if (tlv.length < 32) return null
  const dv = new DataView(tlv.value)
  return {
    exceptionType: dv.getUint8(0),
    exceptionTypeName: getExceptionTypeName(dv.getUint8(0)),
    pc: readU32LE(dv, 4),
    sp: readU32LE(dv, 8),
    lr: readU32LE(dv, 12),
    r0: readU32LE(dv, 16),
    r1: readU32LE(dv, 20),
    r2: readU32LE(dv, 24),
    r3: readU32LE(dv, 28),
    xPSR: readU32LE(dv, 32),
    faultStatus: dv.getUint8(36),
    faultAddress: readU32LE(dv, 40),
    stackTrace: [], // would need variable-length parsing
  }
}

function getExceptionTypeName(n: number): string {
  const names: Record<number, string> = {
    1: 'Reset',
    2: 'NMI',
    3: 'HardFault',
    4: 'MemManage',
    5: 'BusFault',
    6: 'UsageFault',
    7: 'SecureFault',
    8: 'Reserved',
    10: 'SVCall',
    11: 'DebugMonitor',
    12: 'Reserved',
    13: 'PendSV',
    14: 'SysTick',
  }
  return names[n] || `Exception ${n}`
}

// Main decode function
export function decodeLEP(hexString: string): LEPEnvelope {
  try {
    // Clean hex string: remove 0x prefix, then strip non-hex chars
    let clean = hexString.replace(/^0x/i, '').replace(/[^0-9a-fA-F]/g, '')
    if (clean.length % 2 !== 0) {
      return { valid: false, error: 'Invalid hex string (odd length)', header: emptyHeader(), tlvs: [], raw: new ArrayBuffer(0) }
    }

    const bytes = new Uint8Array(clean.length / 2)
    for (let i = 0; i < clean.length; i += 2) {
      bytes[i / 2] = parseInt(clean.slice(i, i + 2), 16)
    }

    if (bytes.length < HEADER_SIZE + 4) {
      return { valid: false, error: 'Payload too short for LEP header (need at least 28 bytes)', header: emptyHeader(), tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
    }

    const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
    const header = parseHeader(dv, bytes)
    if (!header.isValid) {
      if (header.magic !== 'LSTP') {
        return { valid: false, error: 'Invalid magic (expected LSTP)', header, tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
      }
      if (header.version < VERSION_1 || header.version > CURRENT_VERSION) {
        return { valid: false, error: `Unsupported LEP version ${header.version}`, header, tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
      }
      return { valid: false, error: 'Header CRC mismatch', header, tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
    }

    // Encrypted payloads are not decoded client-side.
    if (header.flags & FLAG_ENC) {
      return { valid: true, error: 'Encrypted payload (not decoded client-side)', header, tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
    }

    const payloadStart = HEADER_SIZE
    const payloadEnd = payloadStart + header.payloadLength
    if (payloadEnd + 4 > bytes.length) {
      return { valid: false, error: 'Envelope shorter than declared payload length', header, tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
    }
    if (readU32LE(dv, payloadEnd) !== crc32(bytes.slice(payloadStart, payloadEnd).buffer)) {
      return { valid: false, error: 'Payload CRC mismatch', header, tlvs: [], raw: bytes.buffer.slice(0, bytes.byteLength) }
    }

    const payloadBytes = bytes.slice(payloadStart, payloadEnd)
    const tlvs = parseTLVs(payloadBytes, 0, payloadBytes.length)

    return { header, tlvs, raw: bytes.buffer.slice(0, bytes.byteLength), valid: true }
  } catch (e: any) {
    return { valid: false, error: e.message || 'Parse error', header: emptyHeader(), tlvs: [], raw: new ArrayBuffer(0) }
  }
}

// Encode a LEP envelope from components (for testing/generation).
// Emits the current wire version (2). A v1 header may be requested by passing
// the header object form (not used by the explorer sample path).
export function encodeLEP(tlvs: { type: number; value: Bytes }[]): Bytes
export function encodeLEP(opts: { version: number; eventType: number; architecture: number; flags: number; sequence: number; eventId: number; tlvs: { type: number; value: Bytes }[] }): Bytes
export function encodeLEP(input: { type: number; value: Bytes }[] | { version: number; eventType: number; architecture: number; flags: number; sequence: number; eventId: number; tlvs: { type: number; value: Bytes }[] }): Bytes {
  const tlvs = Array.isArray(input) ? input : input.tlvs
  const version = Array.isArray(input) ? CURRENT_VERSION : (input.version || CURRENT_VERSION)
  const eventType = Array.isArray(input) ? 3 : input.eventType
  const architecture = Array.isArray(input) ? 0 : input.architecture
  const flags = Array.isArray(input) ? 0 : input.flags
  const sequence = Array.isArray(input) ? 0 : input.sequence
  const eventId = Array.isArray(input) ? 0 : input.eventId

  // Calculate payload length
  let payloadLen = 0
  for (const tlv of tlvs) {
    payloadLen += 4 + tlv.value.byteLength
  }

  // Build header (24 bytes, CRC placeholder)
  const header = new ArrayBuffer(HEADER_SIZE)
  const hv = new DataView(header)
  hv.setUint8(0, 0x4C) // 'L'
  hv.setUint8(1, 0x53) // 'S'
  hv.setUint8(2, 0x54) // 'T'
  hv.setUint8(3, 0x50) // 'P'
  hv.setUint8(4, version)
  hv.setUint8(5, eventType)
  hv.setUint8(6, architecture)
  hv.setUint8(7, flags & ~(FLAG_AUTH | FLAG_ENC | FLAG_AEAD))
  hv.setUint32(8, sequence, true)
  hv.setUint32(12, eventId, true)
  hv.setUint32(16, payloadLen, true)
  hv.setUint32(20, 0, true) // CRC placeholder

  // Build payload
  const payload = new ArrayBuffer(payloadLen)
  const pv = new DataView(payload)
  let offset = 0
  for (const tlv of tlvs) {
    pv.setUint16(offset, tlv.type, true)
    pv.setUint16(offset + 2, tlv.value.byteLength, true)
    new Uint8Array(payload, offset + 4).set(new Uint8Array(tlv.value))
    offset += 4 + tlv.value.byteLength
  }

  // Compute and insert header CRC (over header[0:20]) and payload CRC
  const headerCrc = crc32(header.slice(0, 20))
  hv.setUint32(20, headerCrc, true)

  const out = new Uint8Array(header.byteLength + payload.byteLength + 4)
  out.set(new Uint8Array(header), 0)
  out.set(new Uint8Array(payload), header.byteLength)
  const payloadCrc = crc32(payload)
  const pvOut = new DataView(out.buffer)
  pvOut.setUint32(header.byteLength + payload.byteLength, payloadCrc, true)

  return out.buffer
}

// Format bytes as hex string
export function toHex(buf: Bytes): string {
  return Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, '0')).join(' ')
}