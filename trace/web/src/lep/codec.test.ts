// LEP Codec tests
// Run with: npx tsx src/lep/codec.test.ts

import { decodeLEP, encodeLEP, toHex, parseCrashInfo, FLAG_AUTH, FLAG_ENC, FLAG_COMPRESSED, TLV_CRASH, TLV_BREADCRUMB, TLV_METRIC, TLV_ANOMALY, CURRENT_VERSION } from './codec'

function assert(cond: unknown, msg: string) {
  if (!cond) throw new Error(`Assertion failed: ${msg}`)
}

function assertEq<T>(actual: T, expected: T, msg: string) {
  if (actual !== expected) {
    throw new Error(`Assertion failed: ${msg} (expected ${expected}, got ${actual})`)
  }
}

// Test decodeLEP with empty string
const empty = decodeLEP('')
assert(!empty.valid, 'empty hex should be invalid')
assert(empty.error !== undefined, 'empty hex should have error')

// Test decodeLEP with odd-length hex
const odd = decodeLEP('4C53')
assert(!odd.valid, 'odd-length hex should be invalid')
assert(odd.error !== undefined, 'odd-length hex should have error')

// Test decodeLEP with too-short hex
const short = decodeLEP('4C53545001000000000001')
assert(!short.valid, 'too-short hex should be invalid')

// Test decodeLEP with invalid magic
const badMagic = decodeLEP('00000000010000000000000000000000000000000000000000000000')
assert(!badMagic.valid, 'bad magic should be invalid')

// Test decodeLEP with valid header (LSTP magic, version 1, 0 payload)
// Header: LSTP (4) + version u16 LE (1) + payload_len u32 LE (0) + flags u8 (0) + CRC u32 LE
// CRC of first 12 bytes: we need to compute it
// For a simple test, just verify the structure parsing doesn't crash
const validHeaderHex = '4C535450010000000000000000000000000000000000000000000000000000000000'
const valid = decodeLEP(validHeaderHex)
// This may not pass CRC validation but should parse the header
assertEq(valid.header.magic, 'LSTP', 'valid header magic')
assertEq(valid.header.version, 1, 'valid header version')

// Test encodeLEP with a simple TLV
const breadcrumbValue = new TextEncoder().encode('Hello World').buffer
const encoded = encodeLEP([{ type: TLV_BREADCRUMB, value: breadcrumbValue }])
assert(encoded.byteLength > 24, 'encoded should be larger than header')

// Test toHex
const testBuf = new Uint8Array([0x4C, 0x53, 0x54, 0x50]).buffer
const hex = toHex(testBuf)
assertEq(hex, '4c 53 54 50', 'toHex basic')

// Test toHex with different bytes
const hex2 = toHex(new Uint8Array([0xAB, 0xCD]).buffer)
assertEq(hex2, 'ab cd', 'toHex with spaces')

// Test parseCrashInfo with valid data
const crashData = new ArrayBuffer(44)
const cdv = new DataView(crashData)
cdv.setUint8(0, 3) // HardFault
cdv.setUint32(4, 0x08001234, true) // PC
cdv.setUint32(8, 0x2001FF00, true) // SP
cdv.setUint32(12, 0x08005678, true) // LR
cdv.setUint32(16, 0x00000000, true) // R0
cdv.setUint32(20, 0x00000001, true) // R1
cdv.setUint32(24, 0x00000002, true) // R2
cdv.setUint32(28, 0x00000003, true) // R3
cdv.setUint32(32, 0x01000000, true) // xPSR
cdv.setUint8(36, 0x02) // Fault Status
cdv.setUint32(40, 0x08001000, true) // Fault Address

const tlv = {
  type: TLV_CRASH,
  typeName: 'Crash',
  length: 44,
  value: crashData,
  hex: '',
}

const crashInfo = parseCrashInfo(tlv)
assert(crashInfo !== null, 'crash info should not be null')
assertEq(crashInfo!.exceptionType, 3, 'exception type HardFault')
assertEq(crashInfo!.exceptionTypeName, 'HardFault', 'exception type name')
assertEq(crashInfo!.pc, 0x08001234, 'PC value')
assertEq(crashInfo!.sp, 0x2001FF00, 'SP value')
assertEq(crashInfo!.lr, 0x08005678, 'LR value')
assertEq(crashInfo!.r0, 0, 'R0 value')
assertEq(crashInfo!.r1, 1, 'R1 value')
assertEq(crashInfo!.r2, 2, 'R2 value')
assertEq(crashInfo!.r3, 3, 'R3 value')
assertEq(crashInfo!.faultStatus, 0x02, 'fault status')

// Test parseCrashInfo with too-short data
const shortCrash = {
  type: TLV_CRASH,
  typeName: 'Crash',
  length: 10,
  value: new ArrayBuffer(10),
  hex: '',
}
assert(parseCrashInfo(shortCrash) === null, 'short crash should return null')

// Test flag constants
assertEq(FLAG_AUTH, 0x01, 'FLAG_AUTH')
assertEq(FLAG_ENC, 0x02, 'FLAG_ENC')
assertEq(FLAG_COMPRESSED, 0x10, 'FLAG_COMPRESSED')

// Test TLV type constants (laststate/protocol registry)
assertEq(TLV_CRASH, 0x05, 'TLV_CRASH (= Fault)')
assertEq(TLV_BREADCRUMB, 0x06, 'TLV_BREADCRUMB')
assertEq(TLV_METRIC, 0x07, 'TLV_METRIC')
assertEq(TLV_ANOMALY, 0x05, 'TLV_ANOMALY (= Fault alias)')

// Test encode -> decode round trip (emits v2 by default)
const rt = encodeLEP([{ type: TLV_BREADCRUMB, value: new TextEncoder().encode('roundtrip').buffer }])
const rtEnv = decodeLEP(toHex(rt))
assert(rtEnv.valid, 'round-trip envelope should be valid')
assertEq(rtEnv.header.version, CURRENT_VERSION, 'round-trip version')
assertEq(rtEnv.tlvs.length, 1, 'round-trip tlv count')
assertEq(rtEnv.tlvs[0].type, TLV_BREADCRUMB, 'round-trip tlv type')

// Test decodeLEP with hex that has spaces (needs full header: 16 bytes = 32 hex chars)
const withSpaces = decodeLEP('4C 53 54 50 01 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00')
assertEq(withSpaces.header.magic, 'LSTP', 'decodeLEP handles spaces')

// Test decodeLEP with 0x prefix (needs full header)
const withPrefix = decodeLEP('0x4C535450010000000000000000000000000000000000000000000000')
assertEq(withPrefix.header.magic, 'LSTP', 'decodeLEP handles 0x prefix')

// Test encodeLEP with multiple TLVs
const tlv1 = { type: TLV_BREADCRUMB, value: new TextEncoder().encode('Hello').buffer }
const tlv2 = { type: TLV_METRIC, value: new ArrayBuffer(4) }
const multiEncoded = encodeLEP([tlv1, tlv2])
assert(multiEncoded.byteLength > 24, 'multi TLV encoded should be larger than header')

console.log('lep/codec.test.ts: all tests passed')
