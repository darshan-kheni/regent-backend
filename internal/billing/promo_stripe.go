package billing

import (
	"fmt"
	"log/slog"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/coupon"
	"github.com/stripe/stripe-go/v84/subscription"

	"github.com/darshan-kheni/regent/internal/database"
)

// applyDiscount creates a Stripe coupon and applies it to the tenant's subscription.
func (s *PromoService) applyDiscount(ctx database.TenantContext, promo PromoCode) error {
	if promo.DiscountPercent == nil {
		return fmt.Errorf("discount promo missing percent")
	}

	// Look up the tenant's Stripe subscription ID
	var subID *string
	err := s.pool.QueryRow(ctx,
		"SELECT stripe_subscription_id FROM tenants WHERE id = $1",
		ctx.TenantID,
	).Scan(&subID)
	if err != nil {
		return fmt.Errorf("querying tenant subscription: %w", err)
	}
	if subID == nil || *subID == "" {
		return fmt.Errorf("tenant has no active Stripe subscription")
	}

	// Create a one-time Stripe coupon
	cpn, err := coupon.New(&stripe.CouponParams{
		PercentOff: stripe.Float64(float64(*promo.DiscountPercent)),
		Duration:   stripe.String(string(stripe.CouponDurationOnce)),
		Name:       stripe.String(fmt.Sprintf("Promo: %s", promo.Code)),
	})
	if err != nil {
		return fmt.Errorf("creating Stripe coupon: %w", err)
	}

	slog.Info("billing: created Stripe coupon for promo",
		"coupon_id", cpn.ID,
		"code", promo.Code,
		"percent_off", *promo.DiscountPercent,
		"tenant_id", ctx.TenantID,
	)

	// Apply coupon to subscription
	_, err = subscription.Update(*subID, &stripe.SubscriptionParams{
		Discounts: []*stripe.SubscriptionDiscountParams{
			{Coupon: stripe.String(cpn.ID)},
		},
	})
	if err != nil {
		return fmt.Errorf("applying coupon to subscription: %w", err)
	}

	slog.Info("billing: applied promo discount to subscription",
		"subscription_id", *subID,
		"coupon_id", cpn.ID,
		"tenant_id", ctx.TenantID,
	)

	return nil
}
