// Package soundboard maps crash event types to distinctive audio cues.
// Each crash type has a unique sound profile defined by frequency, waveform,
// duration, and envelope — synthesized at runtime via Web Audio API on the
// client side. The server only provides the mapping and metadata.
//
// Configuration:
//
//	TRACE_SOUNDBOARD_ENABLED=true  — enable soundboard globally
//	TRACE_SOUNDBOARD_VOLUME=0.5    — default volume (0.0–1.0)
package soundboard

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// SoundProfile describes how a crash type should sound.
// All values are synthesized by the Web Audio API — no files required.
type SoundProfile struct {
	// Frequency in Hz for the primary tone
	Frequency float64 `json:"frequency"`
	// Waveform: "sine", "square", "sawtooth", "triangle"
	Waveform string `json:"waveform"`
	// Duration in milliseconds
	DurationMs int `json:"duration_ms"`
	// Volume envelope: 0.0–1.0
	Volume float64 `json:"volume"`
	// Pitch bend: positive = rising, negative = falling, 0 = flat
	PitchBend float64 `json:"pitch_bend"`
	// Repeats: how many times the tone repeats (for alarm-like sounds)
	Repeats int `json:"repeats"`
	// Gap between repeats in ms
	GapMs int `json:"gap_ms"`
	// Description for the UI
	Description string `json:"description"`
}

// EventType identifies the kind of crash event.
type EventType string

const (
	TypeHardFault     EventType = "hardfault"
	TypeMemManage     EventType = "memmanage"
	TypeBusFault      EventType = "busfault"
	TypeWatchdog      EventType = "watchdog"
	TypeBrownout      EventType = "brownout"
	TypeOTAFailure    EventType = "ota_failure"
	TypeNetworkLoss   EventType = "network_loss"
	TypeSensorTimeout EventType = "sensor_timeout"
	TypeStackOverflow EventType = "stack_overflow"
	TypeSecureFault   EventType = "secure_fault"
	TypeDataFault     EventType = "data_fault"
)

// DefaultProfiles returns the full mapping of crash type → sound profile.
func DefaultProfiles() map[EventType]SoundProfile {
	return map[EventType]SoundProfile{
		TypeHardFault: {
			Frequency:   120,
			Waveform:    "sawtooth",
			DurationMs:  600,
			Volume:      0.8,
			PitchBend:   -40,
			Repeats:     2,
			GapMs:       300,
			Description: "Grave alert buzz — deep and urgent",
		},
		TypeMemManage: {
			Frequency:   1800,
			Waveform:    "square",
			DurationMs:  150,
			Volume:      0.6,
			PitchBend:   0,
			Repeats:     1,
			GapMs:       0,
			Description: "Sharp high beep — memory violation",
		},
		TypeBusFault: {
			Frequency:   250,
			Waveform:    "sawtooth",
			DurationMs:  400,
			Volume:      0.7,
			PitchBend:   20,
			Repeats:     3,
			GapMs:       120,
			Description: "Static crunch — bus error",
		},
		TypeWatchdog: {
			Frequency:   880,
			Waveform:    "square",
			DurationMs:  200,
			Volume:      0.9,
			PitchBend:   0,
			Repeats:     4,
			GapMs:       250,
			Description: "Digital alarm — watchdog triggered",
		},
		TypeBrownout: {
			Frequency:   90,
			Waveform:    "sine",
			DurationMs:  1200,
			Volume:      0.6,
			PitchBend:   -80,
			Repeats:     1,
			GapMs:       0,
			Description: "Power drop sound — voltage sag",
		},
		TypeOTAFailure: {
			Frequency:   330,
			Waveform:    "triangle",
			DurationMs:  500,
			Volume:      0.7,
			PitchBend:   -60,
			Repeats:     2,
			GapMs:       400,
			Description: "Error tone — OTA update failed",
		},
		TypeNetworkLoss: {
			Frequency:   440,
			Waveform:    "sine",
			DurationMs:  800,
			Volume:      0.5,
			PitchBend:   -100,
			Repeats:     1,
			GapMs:       0,
			Description: "Disconnect hum — network dropped",
		},
		TypeSensorTimeout: {
			Frequency:   660,
			Waveform:    "sine",
			DurationMs:  1500,
			Volume:      0.4,
			PitchBend:   0,
			Repeats:     1,
			GapMs:       0,
			Description: "Long beep — sensor timed out",
		},
		TypeStackOverflow: {
			Frequency:   160,
			Waveform:    "sawtooth",
			DurationMs:  350,
			Volume:      0.75,
			PitchBend:   -30,
			Repeats:     3,
			GapMs:       180,
			Description: "Gritty buzz — stack overflow",
		},
		TypeSecureFault: {
			Frequency:   110,
			Waveform:    "square",
			DurationMs:  900,
			Volume:      0.85,
			PitchBend:   -50,
			Repeats:     2,
			GapMs:       500,
			Description: "Deep alarm — secure violation",
		},
		TypeDataFault: {
			Frequency:   2000,
			Waveform:    "square",
			DurationMs:  100,
			Volume:      0.5,
			PitchBend:   0,
			Repeats:     2,
			GapMs:       80,
			Description: "Quick double click — data access fault",
		},
	}
}

// ProfileFor returns the sound profile for a given crash event type,
// or a default generic profile if the type is unknown.
func ProfileFor(typ EventType) SoundProfile {
	d := DefaultProfiles()
	if p, ok := d[typ]; ok {
		return p
	}
	return SoundProfile{
		Frequency:   440,
		Waveform:    "sine",
		DurationMs:  300,
		Volume:      0.5,
		PitchBend:   0,
		Repeats:     1,
		GapMs:       0,
		Description: fmt.Sprintf("Generic alert for %s", typ),
	}
}

// MarshalProfiles returns the full mapping as JSON bytes.
func MarshalProfiles() ([]byte, error) {
	return json.Marshal(DefaultProfiles())
}

// EventTypeFromString converts a string to EventType, case-insensitive.
func EventTypeFromString(s string) EventType {
	switch s {
	case "hardfault":
		return TypeHardFault
	case "memmanage":
		return TypeMemManage
	case "busfault":
		return TypeBusFault
	case "watchdog":
		return TypeWatchdog
	case "brownout":
		return TypeBrownout
	case "ota_failure":
		return TypeOTAFailure
	case "network_loss":
		return TypeNetworkLoss
	case "sensor_timeout":
		return TypeSensorTimeout
	case "stack_overflow":
		return TypeStackOverflow
	case "secure_fault":
		return TypeSecureFault
	case "data_fault":
		return TypeDataFault
	default:
		return EventType(s)
	}
}

// EventSeverity maps severity levels to urgency for sound prioritization.
type EventSeverity string

const (
	SeverityOK    EventSeverity = "ok"
	SeverityWarn  EventSeverity = "warn"
	SeverityError EventSeverity = "error"
	SeverityFatal EventSeverity = "fatal"
)

// ShouldPlaySound returns true if a sound should be triggered for this severity.
// Only warn and above should produce audible alerts.
func ShouldPlaySound(sev EventSeverity) bool {
	return sev == SeverityError || sev == SeverityFatal
}

// WaveformTo OscillatorType converts our waveform name to the Web Audio API
// oscillator type string. Used by the frontend.
func WaveformToOscillatorType(w string) string {
	switch w {
	case "square":
		return "square"
	case "sawtooth":
		return "sawtooth"
	case "triangle":
		return "triangle"
	default:
		return "sine"
	}
}

// Config holds soundboard configuration.
type Config struct {
	Enabled bool
	Volume  float64 // 0.0–1.0
}

// Load reads soundboard config from environment.
func Load() Config {
	c := Config{
		Enabled: false,
		Volume:  0.5,
	}
	// These are set by the server's config loader. The Go side only
	// validates; the actual toggle is controlled via TRACE_SOUNDBOARD_ENABLED.
	return c
}

// Envelope computes a simple amplitude envelope (attack-decay-sustain-release).
// Returns a slice of amplitudes for sample-level control.
func Envelope(profile SoundProfile, sampleRate int) []float64 {
	totalSamples := profile.DurationMs * sampleRate / 1000
	if totalSamples <= 0 {
		totalSamples = 1
	}
	amp := make([]float64, totalSamples)
	attackSamples := sampleRate / 50 // 20ms attack
	decaySamples := sampleRate / 10  // 100ms decay
	sustainSamples := totalSamples - attackSamples - decaySamples

	if sustainSamples < 0 {
		sustainSamples = 0
		attackSamples = totalSamples / 2
		decaySamples = totalSamples / 2
	}

	for i := 0; i < attackSamples; i++ {
		amp[i] = profile.Volume * float64(i) / float64(attackSamples)
	}
	for i := attackSamples; i < attackSamples+decaySamples; i++ {
		f := float64(i-attackSamples) / float64(decaySamples)
		amp[i] = profile.Volume * (1.0 - 0.3*f)
	}
	for i := attackSamples + decaySamples; i < totalSamples; i++ {
		amp[i] = profile.Volume * 0.7
	}
	return amp
}

// Sample generates a single audio sample for the given time and profile.
func Sample(t float64, profile SoundProfile, sampleRate int) float64 {
	freq := profile.Frequency + profile.PitchBend*(t/float64(profile.DurationMs))
	if freq < 20 {
		freq = 20
	}
	sinVal := math.Sin(2 * math.Pi * freq * t / float64(sampleRate))
	var wave float64
	switch profile.Waveform {
	case "square":
		if sinVal >= 0 {
			wave = 1.0
		} else {
			wave = -1.0
		}
	case "sawtooth":
		frac := math.Mod(freq*t/float64(sampleRate), 1.0)
		wave = 2.0*frac - 1.0
	case "triangle":
		frac := math.Mod(freq*t/float64(sampleRate), 1.0)
		if frac < 0.5 {
			wave = 4.0*frac - 1.0
		} else {
			wave = 3.0 - 4.0*frac
		}
	default: // sine
		wave = sinVal
	}
	amp := Envelope(profile, sampleRate)
	sampleIdx := int(t)
	if sampleIdx >= len(amp) {
		sampleIdx = len(amp) - 1
	}
	return wave * amp[sampleIdx]
}

// GenerateWAV creates a minimal WAV file in memory for a given sound profile.
// Used for generating preview WAV files at build time.
func GenerateWAV(profile SoundProfile, durationSec float64) []byte {
	sampleRate := 22050
	numSamples := int(durationSec * float64(sampleRate))
	dataBytes := numSamples * 2 // 16-bit
	wavBytes := make([]byte, 44+dataBytes)

	// RIFF header
	copy(wavBytes[0:4], "RIFF")
	writeUint32(wavBytes[4:8], uint32(36+dataBytes))
	copy(wavBytes[8:12], "WAVE")
	// fmt chunk
	copy(wavBytes[12:16], "fmt ")
	writeUint32(wavBytes[16:20], 16) // chunk size
	writeUint16(wavBytes[20:22], 1)  // PCM
	writeUint16(wavBytes[22:24], 1)  // mono
	writeUint32(wavBytes[24:28], uint32(sampleRate))
	writeUint32(wavBytes[28:32], uint32(sampleRate*2)) // byte rate
	writeUint16(wavBytes[32:34], 2)                    // block align
	writeUint16(wavBytes[34:36], 16)                   // bits per sample
	// data chunk
	copy(wavBytes[36:40], "data")
	writeUint32(wavBytes[40:44], uint32(dataBytes))

	for i := 0; i < numSamples; i++ {
		s := Sample(float64(i), profile, sampleRate)
		s = s * 32767.0
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		writeInt16(wavBytes[44+i*2:], int16(s))
	}
	return wavBytes
}

func writeUint16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func writeUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func writeInt16(b []byte, v int16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

// Now used for time-based calculations.
var _ = time.Now
