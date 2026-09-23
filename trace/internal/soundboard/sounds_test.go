package soundboard

import (
	"encoding/json"
	"math"
	"testing"
)

func TestDefaultProfiles(t *testing.T) {
	profiles := DefaultProfiles()

	expectedTypes := []EventType{
		TypeHardFault, TypeMemManage, TypeBusFault, TypeWatchdog,
		TypeBrownout, TypeOTAFailure, TypeNetworkLoss, TypeSensorTimeout,
		TypeStackOverflow, TypeSecureFault, TypeDataFault,
	}
	for _, et := range expectedTypes {
		if _, ok := profiles[et]; !ok {
			t.Errorf("missing profile for %s", et)
		}
	}
}

func TestProfileFor(t *testing.T) {
	// Known type
	p := ProfileFor(TypeHardFault)
	if p.Frequency != 120 {
		t.Errorf("hardfault frequency should be 120, got %f", p.Frequency)
	}
	if p.Waveform != "sawtooth" {
		t.Errorf("hardfault waveform should be sawtooth, got %s", p.Waveform)
	}

	// Unknown type returns default
	p = ProfileFor(EventType("unknown_type"))
	if p.Frequency != 440 {
		t.Errorf("default frequency should be 440, got %f", p.Frequency)
	}
}

func TestEventTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  EventType
	}{
		{"hardfault", TypeHardFault},
		{"memmanage", TypeMemManage},
		{"busfault", TypeBusFault},
		{"watchdog", TypeWatchdog},
		{"brownout", TypeBrownout},
		{"ota_failure", TypeOTAFailure},
		{"network_loss", TypeNetworkLoss},
		{"sensor_timeout", TypeSensorTimeout},
		{"stack_overflow", TypeStackOverflow},
		{"secure_fault", TypeSecureFault},
		{"data_fault", TypeDataFault},
		{"HARDFLOAT", EventType("HARDFLOAT")}, // case sensitive
		{"unknown", EventType("unknown")},
	}
	for _, tc := range tests {
		got := EventTypeFromString(tc.input)
		if got != tc.want {
			t.Errorf("EventTypeFromString(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestShouldPlaySound(t *testing.T) {
	if ShouldPlaySound(SeverityOK) {
		t.Error("OK should not play sound")
	}
	if ShouldPlaySound(SeverityWarn) {
		t.Error("WARN should not play sound")
	}
	if !ShouldPlaySound(SeverityError) {
		t.Error("ERROR should play sound")
	}
	if !ShouldPlaySound(SeverityFatal) {
		t.Error("FATAL should play sound")
	}
}

func TestWaveformToOscillatorType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sine", "sine"},
		{"square", "square"},
		{"sawtooth", "sawtooth"},
		{"triangle", "triangle"},
		{"unknown", "sine"},
		{"", "sine"},
	}
	for _, tc := range tests {
		got := WaveformToOscillatorType(tc.input)
		if got != tc.want {
			t.Errorf("WaveformToOscillatorType(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestEnvelope(t *testing.T) {
	profile := DefaultProfiles()[TypeHardFault]
	samples := Envelope(profile, 22050)
	if len(samples) == 0 {
		t.Error("envelope should not be empty")
	}
	// Check attack phase is increasing
	if samples[1] < samples[0] {
		t.Error("attack phase should be increasing")
	}
	// Check sustain phase is non-zero
	midIdx := len(samples) / 2
	if samples[midIdx] <= 0 {
		t.Error("sustain phase should be non-zero")
	}
}

func TestSample(t *testing.T) {
	profile := DefaultProfiles()[TypeHardFault]
	sampleRate := 22050

	// Test a few samples
	for i := 0; i < 100; i++ {
		s := Sample(float64(i), profile, sampleRate)
		if math.IsNaN(s) || math.IsInf(s, 0) {
			t.Errorf("sample %d produced NaN or Inf: %f", i, s)
		}
	}
}

func TestGenerateWAV(t *testing.T) {
	profile := DefaultProfiles()[TypeHardFault]
	wav := GenerateWAV(profile, 0.5)
	if len(wav) < 44 {
		t.Error("WAV should have at least 44-byte header")
	}
	// Check RIFF header
	if string(wav[0:4]) != "RIFF" {
		t.Error("WAV should start with RIFF")
	}
	if string(wav[8:12]) != "WAVE" {
		t.Error("WAV should contain WAVE")
	}
}

func TestMarshalProfiles(t *testing.T) {
	data, err := MarshalProfiles()
	if err != nil {
		t.Fatalf("marshal should not error: %v", err)
	}
	if len(data) == 0 {
		t.Error("marshaled data should not be empty")
	}
	// Verify it's valid JSON
	var profiles map[string]SoundProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		t.Errorf("marshaled data should be valid JSON: %v", err)
	}
	if len(profiles) != 11 {
		t.Errorf("expected 11 profiles, got %d", len(profiles))
	}
}

func TestConfigLoad(t *testing.T) {
	c := Load()
	if c.Enabled {
		t.Error("soundboard should be disabled by default")
	}
	if c.Volume != 0.5 {
		t.Errorf("default volume should be 0.5, got %f", c.Volume)
	}
}
