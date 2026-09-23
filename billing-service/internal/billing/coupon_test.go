package billing

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/billing-service/internal/store"
)

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

func seedCoupon(t *testing.T, svc *Service, coupon *store.Coupon) {
	t.Helper()
	if err := svc.db.CreateCoupon(context.Background(), coupon); err != nil {
		t.Fatalf("CreateCoupon: %v", err)
	}
}

func TestValidateCouponPercent(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "LAUNCH20", PercentOff: intPtr(20),
		Currency: "usd", MaxPerOrganization: 1, Active: true, CreatedAt: time.Now(),
	})

	_, discount, err := svc.ValidateCoupon(ctx, "launch20", org, 4900, "usd")
	if err != nil {
		t.Fatalf("ValidateCoupon: %v", err)
	}
	if discount.DiscountCents != 980 || discount.DiscountedCents != 3920 {
		t.Fatalf("discount = %+v, want 980 off / 3920 due", discount)
	}
}

func TestValidateCouponRoundsHalfUp(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()
	// 10% of 999 cents = 99.9 -> 100 cents (half-up).
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "TEN", PercentOff: intPtr(10),
		Currency: "usd", MaxPerOrganization: 1, Active: true, CreatedAt: time.Now(),
	})

	_, discount, err := svc.ValidateCoupon(ctx, "TEN", org, 999, "usd")
	if err != nil {
		t.Fatalf("ValidateCoupon: %v", err)
	}
	if discount.DiscountCents != 100 {
		t.Fatalf("discount = %d, want 100 (half-up)", discount.DiscountCents)
	}
}

func TestValidateCouponFixedAmountAndCurrency(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "BRL50", AmountOffCents: intPtr(5000),
		Currency: "brl", MaxPerOrganization: 1, Active: true, CreatedAt: time.Now(),
	})

	// Same currency: full discount applies.
	_, discount, err := svc.ValidateCoupon(ctx, "BRL50", org, 9000, "brl")
	if err != nil {
		t.Fatalf("ValidateCoupon: %v", err)
	}
	if discount.DiscountCents != 5000 {
		t.Fatalf("discount = %d, want 5000", discount.DiscountCents)
	}

	// Currency mismatch: coupon is valid but discount does not apply.
	_, discount, err = svc.ValidateCoupon(ctx, "BRL50", org, 9000, "usd")
	if err != nil {
		t.Fatalf("ValidateCoupon currency mismatch: %v", err)
	}
	if discount.DiscountCents != 0 {
		t.Fatalf("currency mismatch should not discount, got %d", discount.DiscountCents)
	}
}

func TestValidateCouponNeverGoesNegative(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "BIG", AmountOffCents: intPtr(99999),
		Currency: "usd", MaxPerOrganization: 1, Active: true, CreatedAt: time.Now(),
	})

	_, discount, err := svc.ValidateCoupon(ctx, "BIG", org, 900, "usd")
	if err != nil {
		t.Fatalf("ValidateCoupon: %v", err)
	}
	if discount.DiscountedCents != 0 {
		t.Fatalf("discounted = %d, want 0 (never negative)", discount.DiscountedCents)
	}
}

func TestValidateCouponExpiredOrInactiveOrUnknown(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()

	expired := time.Now().Add(-time.Hour)
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "OLD", PercentOff: intPtr(10), Currency: "usd",
		ExpiresAt: &expired, MaxPerOrganization: 1, Active: true, CreatedAt: time.Now(),
	})
	if _, _, err := svc.ValidateCoupon(ctx, "OLD", org, 1000, "usd"); err == nil {
		t.Fatal("expired coupon should be rejected")
	}

	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "OFF", PercentOff: intPtr(10), Currency: "usd",
		MaxPerOrganization: 1, Active: false, CreatedAt: time.Now(),
	})
	if _, _, err := svc.ValidateCoupon(ctx, "OFF", org, 1000, "usd"); err == nil {
		t.Fatal("inactive coupon should be rejected")
	}

	if _, _, err := svc.ValidateCoupon(ctx, "NOPE", org, 1000, "usd"); err == nil {
		t.Fatal("unknown coupon should be rejected")
	}
}

func TestValidateCouponGlobalCap(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "ONE", PercentOff: intPtr(10), Currency: "usd",
		MaxRedemptions: int64Ptr(1), MaxPerOrganization: 5, Active: true, CreatedAt: time.Now(),
	})

	if _, _, err := svc.ValidateCoupon(ctx, "ONE", org, 1000, "usd"); err != nil {
		t.Fatalf("first validation should pass: %v", err)
	}
	// Simulate the redemption counting against the global cap.
	coupon, _ := svc.db.GetCouponByCode(ctx, "ONE")
	coupon.RedeemedCount = 1
	if _, _, err := svc.ValidateCoupon(ctx, "ONE", uuid.New(), 1000, "usd"); err == nil {
		t.Fatal("exhausted coupon should be rejected")
	}
}

func TestRedeemCouponPerOrgLimit(t *testing.T) {
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()
	seedCoupon(t, svc, &store.Coupon{
		ID: uuid.New(), Code: "ONCE", PercentOff: intPtr(10), Currency: "usd",
		MaxPerOrganization: 1, Active: true, CreatedAt: time.Now(),
	})

	if _, err := svc.RedeemCoupon(ctx, "ONCE", org, 1000, "usd", nil); err != nil {
		t.Fatalf("first redemption should pass: %v", err)
	}
	if _, err := svc.RedeemCoupon(ctx, "ONCE", org, 1000, "usd", nil); err == nil {
		t.Fatal("second redemption by same org should fail")
	}
	// A different organization can still redeem it.
	if _, err := svc.RedeemCoupon(ctx, "ONCE", uuid.New(), 1000, "usd", nil); err != nil {
		t.Fatalf("other org redemption should pass: %v", err)
	}
}
