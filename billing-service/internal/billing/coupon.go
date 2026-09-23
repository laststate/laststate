// Package billing — discount coupons.
//
// A coupon is a case-insensitive alphanumeric code managed by operators
// (seeded directly or via the store API). Exactly one discount shape applies:
// percent_off (1-100) or amount_off_cents (fixed, currency-bound). Redemption
// is capped globally (max_redemptions) and per organization
// (max_per_organization); the global cap is enforced atomically by the store.
package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// CouponDiscount is the computed discount for one amount.
type CouponDiscount struct {
	Code            string `json:"code"`
	OriginalCents   int64  `json:"original_cents"`
	DiscountCents   int64  `json:"discount_cents"`
	DiscountedCents int64  `json:"discounted_cents"`
	Currency        string `json:"currency"`
}

// normalizeCouponCode uppercases and trims the code; coupon codes are
// case-insensitive by definition here.
func normalizeCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// validateCouponState checks the coupon itself (not the org): active, not
// expired, under the global cap, and carrying exactly one discount shape.
func validateCouponState(coupon *store.Coupon, now time.Time) error {
	if !coupon.Active {
		return errors.New("coupon is inactive")
	}
	if coupon.ExpiresAt != nil && !coupon.ExpiresAt.After(now) {
		return errors.New("coupon has expired")
	}
	if coupon.MaxRedemptions != nil && coupon.RedeemedCount >= *coupon.MaxRedemptions {
		return errors.New("coupon redemption limit reached")
	}
	if (coupon.PercentOff == nil) == (coupon.AmountOffCents == nil) {
		return errors.New("coupon must set exactly one of percent_off or amount_off_cents")
	}
	return nil
}

// ValidateCoupon checks whether code is redeemable by orgID and computes the
// discount it would apply to amountCents in the given currency. The amount is
// informational for percent coupons but is currency-checked for fixed-amount
// coupons.
func (s *Service) ValidateCoupon(ctx context.Context, code string, orgID uuid.UUID, amountCents int64, currency string) (*store.Coupon, *CouponDiscount, error) {
	code = normalizeCouponCode(code)
	if code == "" {
		return nil, nil, errors.New("coupon code is required")
	}
	if amountCents < 0 {
		return nil, nil, errors.New("amount must be non-negative")
	}

	coupon, err := s.db.GetCouponByCode(ctx, code)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, errors.New("coupon not found")
		}
		return nil, nil, fmt.Errorf("coupon lookup: %w", err)
	}

	if err := validateCouponState(coupon, time.Now()); err != nil {
		return coupon, nil, err
	}

	orgRedemptions, countErr := s.db.CountOrgCouponRedemptions(ctx, coupon.ID, orgID)
	if countErr != nil {
		return coupon, nil, fmt.Errorf("redemption count: %w", countErr)
	}
	if coupon.MaxPerOrganization > 0 && orgRedemptions >= coupon.MaxPerOrganization {
		return coupon, nil, errors.New("organization already redeemed this coupon")
	}

	discount := applyCouponToAmount(coupon, amountCents, currency)
	return coupon, discount, nil
}

// applyCouponToAmount computes the discount for amountCents. Fixed-amount
// coupons never discount below zero and are rejected on currency mismatch.
func applyCouponToAmount(coupon *store.Coupon, amountCents int64, currency string) *CouponDiscount {
	discountCents := int64(0)
	if coupon.PercentOff != nil {
		// Rounded half-up; the customer never loses a cent to truncation.
		discountCents = (amountCents*int64(*coupon.PercentOff) + 50) / 100
	} else if coupon.AmountOffCents != nil {
		cur := currency
		if cur == "" {
			cur = coupon.Currency
		}
		if strings.EqualFold(cur, coupon.Currency) {
			discountCents = int64(*coupon.AmountOffCents)
		}
	}
	if discountCents > amountCents {
		discountCents = amountCents
	}
	return &CouponDiscount{
		Code:            coupon.Code,
		OriginalCents:   amountCents,
		DiscountCents:   discountCents,
		DiscountedCents: amountCents - discountCents,
		Currency:        currency,
	}
}

// RedeemCoupon validates and atomically redeems code for orgID, returning the
// discount that was applied. Callers should associate the returned discount
// with the invoice they generate next.
func (s *Service) RedeemCoupon(ctx context.Context, code string, orgID uuid.UUID, amountCents int64, currency string, invoiceID *uuid.UUID) (*CouponDiscount, error) {
	coupon, discount, err := s.ValidateCoupon(ctx, code, orgID, amountCents, currency)
	if err != nil {
		return nil, err
	}

	if err := s.db.RedeemCoupon(ctx, coupon.ID, orgID, invoiceID); err != nil {
		if errors.Is(err, store.ErrCouponExhausted) {
			return nil, errors.New("coupon redemption limit reached")
		}
		return nil, fmt.Errorf("redeem coupon: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("coupon redeemed",
			zap.String("coupon", coupon.Code),
			zap.String("organization_id", orgID.String()),
			zap.Int64("discount_cents", discount.DiscountCents))
	}
	return discount, nil
}
