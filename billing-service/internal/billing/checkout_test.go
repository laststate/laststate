package billing

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// --- CreateCheckout ---

func TestPriceEnvForPlan(t *testing.T) {
	for plan, want := range map[string]string{
		"poc":        "BILLING_PRICE_POC",
		"pilot":      "BILLING_PRICE_PILOT",
		"fleet":      "BILLING_PRICE_FLEET",
		"enterprise": "BILLING_PRICE_ENTERPRISE",
		" POC ":      "BILLING_PRICE_POC",
	} {
		env, ok := priceEnvForPlan(plan)
		if !ok || env != want {
			t.Errorf("priceEnvForPlan(%q) = %q, %v; want %q, true", plan, env, ok, want)
		}
	}
	if _, ok := priceEnvForPlan("hobby"); ok {
		t.Error("priceEnvForPlan(hobby) should not resolve")
	}
}

func TestCreateCheckoutUnknownPlan(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreateCheckout(context.Background(), uuid.New(), "stripe", "hobby", "usd")
	if err == nil || !strings.Contains(err.Error(), "unknown plan") {
		t.Fatalf("expected unknown plan error, got %v", err)
	}
}

func TestCreateCheckoutNilOrg(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreateCheckout(context.Background(), uuid.Nil, "stripe", "poc", "usd")
	if err == nil {
		t.Fatal("expected organization_id error")
	}
}

func TestCreateCheckoutMissingPriceNamesEnv(t *testing.T) {
	t.Setenv("BILLING_PRICE_PILOT", "")
	svc := NewService(newMockDB(), NewStripeProvider("sk_test_fake"), nil, nil)
	_, err := svc.CreateCheckout(context.Background(), uuid.New(), "stripe", "pilot", "usd")
	if err == nil || !strings.Contains(err.Error(), "BILLING_PRICE_PILOT") {
		t.Fatalf("expected error naming BILLING_PRICE_PILOT, got %v", err)
	}
}

func TestCreateCheckoutStripeWithoutKey(t *testing.T) {
	t.Setenv("BILLING_PRICE_POC", "price_poc_test")
	svc := NewService(newMockDB(), NewStripeProvider(""), nil, nil)
	_, err := svc.CreateCheckout(context.Background(), uuid.New(), "stripe", "poc", "usd")
	if err == nil || !strings.Contains(err.Error(), "STRIPE_KEY") {
		t.Fatalf("expected STRIPE_KEY error, got %v", err)
	}
}

func TestCreateCheckoutNonStripePassthrough(t *testing.T) {
	svc := NewService(newMockDB(), nil, &mockProvider{}, nil)
	url, err := svc.CreateCheckout(context.Background(), uuid.New(), "mp", "pilot", "usd")
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if url != "mock_session" {
		t.Fatalf("expected mock_session, got %q", url)
	}
}

func TestCreateCheckoutPOCNeedsStripe(t *testing.T) {
	svc := NewService(newMockDB(), nil, &mockProvider{}, nil)
	_, err := svc.CreateCheckout(context.Background(), uuid.New(), "mp", "poc", "usd")
	if err == nil || !strings.Contains(err.Error(), "stripe") {
		t.Fatalf("expected stripe-only error, got %v", err)
	}
}

func TestCreateCheckoutUnknownProvider(t *testing.T) {
	svc := newTestService()
	_, err := svc.CreateCheckout(context.Background(), uuid.New(), "paypal", "pilot", "usd")
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}
