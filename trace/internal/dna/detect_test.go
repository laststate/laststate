package dna

import (
	"context"
	"fmt"
	"testing"
)

type fakeStore struct {
	dnas map[string]DeviceDNA
}

func (f *fakeStore) Save(ctx context.Context, dna DeviceDNA) error {
	f.dnas[dna.DeviceID] = dna
	return nil
}

func (f *fakeStore) Get(ctx context.Context, deviceID string) (*DeviceDNA, error) {
	d, ok := f.dnas[deviceID]
	if !ok {
		return nil, fmt.Errorf("device not found: %s", deviceID)
	}
	return &d, nil
}

func (f *fakeStore) ListAll(ctx context.Context) ([]DeviceDNA, error) {
	result := make([]DeviceDNA, 0, len(f.dnas))
	for _, d := range f.dnas {
		result = append(result, d)
	}
	return result, nil
}

func TestCaptureDNA(t *testing.T) {
	store := &fakeStore{dnas: make(map[string]DeviceDNA)}
	mgr := NewManager(store, 0.95, nil)

	dna, err := mgr.CaptureDNA(context.Background(), "dev-1", []float64{45.0, 46.0, 44.0}, 64.0, 10, "mcu-001", "sig-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dna.DeviceID != "dev-1" {
		t.Errorf("expected device_id dev-1, got %s", dna.DeviceID)
	}
	if dna.BootTimeAvg != 45.0 {
		t.Errorf("expected boot_time_avg 45.0, got %f", dna.BootTimeAvg)
	}
	if dna.ClockFreq != 64.0 {
		t.Errorf("expected clock_freq 64.0, got %f", dna.ClockFreq)
	}
	if dna.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
}

func TestCompareDevices_Identical(t *testing.T) {
	store := &fakeStore{dnas: make(map[string]DeviceDNA)}
	mgr := NewManager(store, 0.95, nil)

	// Capture two identical devices
	mgr.CaptureDNA(context.Background(), "dev-1", []float64{45.0, 46.0}, 64.0, 10, "mcu-001", "sig-001")
	mgr.CaptureDNA(context.Background(), "dev-2", []float64{45.0, 46.0}, 64.0, 10, "mcu-001", "sig-001")

	result, err := mgr.CompareDevices(context.Background(), "dev-1", "dev-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Similarity != 1.0 {
		t.Errorf("expected similarity 1.0, got %f", result.Similarity)
	}
	if !result.Match {
		t.Error("expected match to be true for identical devices")
	}
}

func TestCompareDevices_Different(t *testing.T) {
	store := &fakeStore{dnas: make(map[string]DeviceDNA)}
	mgr := NewManager(store, 0.95, nil)

	mgr.CaptureDNA(context.Background(), "dev-1", []float64{45.0}, 64.0, 10, "mcu-001", "sig-001")
	mgr.CaptureDNA(context.Background(), "dev-2", []float64{100.0}, 32.0, 90, "mcu-999", "sig-999")

	result, err := mgr.CompareDevices(context.Background(), "dev-1", "dev-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Similarity >= 0.95 {
		t.Errorf("expected similarity < 0.95, got %f", result.Similarity)
	}
	if result.Match {
		t.Error("expected match to be false for different devices")
	}
}

func TestDetectClones(t *testing.T) {
	store := &fakeStore{dnas: make(map[string]DeviceDNA)}
	mgr := NewManager(store, 0.95, nil)

	// Capture 3 devices, 2 identical
	mgr.CaptureDNA(context.Background(), "dev-1", []float64{45.0}, 64.0, 10, "mcu-001", "sig-001")
	mgr.CaptureDNA(context.Background(), "dev-2", []float64{45.0}, 64.0, 10, "mcu-001", "sig-001")
	mgr.CaptureDNA(context.Background(), "dev-3", []float64{100.0}, 32.0, 90, "mcu-999", "sig-999")

	clones, err := mgr.DetectClones(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(clones) != 1 {
		t.Errorf("expected 1 clone pair, got %d", len(clones))
	}
	if clones[0].Device1ID != "dev-1" || clones[0].Device2ID != "dev-2" {
		t.Error("expected clone pair to be dev-1 and dev-2")
	}
}

func TestMeanStdDev(t *testing.T) {
	tests := []struct {
		data     []float64
		wantMean float64
		wantStd  float64
	}{
		{[]float64{1, 2, 3, 4, 5}, 3.0, 1.4142135623730951},
		{[]float64{10, 20, 30}, 20.0, 8.16496580927726},
		{[]float64{5}, 5.0, 0},
		{[]float64{}, 0, 0},
	}

	for _, tt := range tests {
		mean, stddev := meanStdDev(tt.data)
		if mean != tt.wantMean {
			t.Errorf("mean of %v: got %f, want %f", tt.data, mean, tt.wantMean)
		}
		if stddev != tt.wantStd {
			t.Errorf("stddev of %v: got %f, want %f", tt.data, stddev, tt.wantStd)
		}
	}
}
