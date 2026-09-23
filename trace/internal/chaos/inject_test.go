package chaos

import (
	"context"
	"testing"
)

type fakeAdapter struct {
	result *InjectionResult
	err    error
}

func (f *fakeAdapter) Inject(ctx context.Context, deviceID string, injectType InjectionType) (*InjectionResult, error) {
	return f.result, f.err
}

func TestInjectFault_Disabled(t *testing.T) {
	mgr := NewManager(Config{Enabled: false}, nil)
	_, err := mgr.InjectFault(context.Background(), "dev-1", TypeHardFault)
	if err == nil {
		t.Error("expected error when chaos is disabled")
	}
}

func TestInjectFault_NoAdapter(t *testing.T) {
	mgr := NewManager(Config{Enabled: true}, nil)
	_, err := mgr.InjectFault(context.Background(), "dev-1", TypeHardFault)
	if err == nil {
		t.Error("expected error when no adapter is set")
	}
}

func TestInjectFault_Success(t *testing.T) {
	fake := &fakeAdapter{
		result: &InjectionResult{
			ID:         "inj-1",
			Type:       TypeHardFault,
			DeviceID:   "dev-1",
			Status:     "success",
			Captured:   true,
			DurationMs: 42,
		},
	}

	mgr := NewManager(Config{Enabled: true}, nil)
	mgr.SetAdapter(fake)

	result, err := mgr.InjectFault(context.Background(), "dev-1", TypeHardFault)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ID != "inj-1" {
		t.Errorf("expected ID inj-1, got %s", result.ID)
	}
	if !result.Captured {
		t.Error("expected captured to be true")
	}
	if result.DurationMs != 42 {
		t.Errorf("expected duration 42, got %d", result.DurationMs)
	}
}

func TestInjectFault_Error(t *testing.T) {
	fake := &fakeAdapter{
		err: context.DeadlineExceeded,
	}

	mgr := NewManager(Config{Enabled: true}, nil)
	mgr.SetAdapter(fake)

	_, err := mgr.InjectFault(context.Background(), "dev-1", TypeHardFault)
	if err == nil {
		t.Error("expected error from adapter")
	}
}

func TestGetResults(t *testing.T) {
	fake := &fakeAdapter{
		result: &InjectionResult{
			ID:     "inj-1",
			Type:   TypeHardFault,
			Status: "success",
		},
	}

	mgr := NewManager(Config{Enabled: true}, nil)
	mgr.SetAdapter(fake)

	// Inject a few results
	for i := 0; i < 5; i++ {
		mgr.InjectFault(context.Background(), "dev-1", TypeHardFault)
	}

	results := mgr.GetResults()
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
}

func TestAllInjectionTypes(t *testing.T) {
	types := AllInjectionTypes()
	if len(types) != 6 {
		t.Errorf("expected 6 injection types, got %d", len(types))
	}

	expected := []InjectionType{
		TypeHardFault, TypeWatchdog, TypeBrownout, TypeCorruptStack, TypeNestedFault, TypeInterruptedFlash,
	}

	for i, e := range expected {
		if types[i] != e {
			t.Errorf("expected type %s at index %d, got %s", e, i, types[i])
		}
	}
}
