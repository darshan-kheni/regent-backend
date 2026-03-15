package billing

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/stripe/stripe-go/v84"
)

// InitStripe initializes the Stripe SDK with the secret key from config.
// Validates that the key prefix matches the configured mode.
func InitStripe(cfg BillingConfig) error {
	if cfg.StripeSecretKey == "" {
		slog.Warn("billing: STRIPE_SECRET_KEY not set, Stripe integration disabled")
		return nil
	}

	mode := cfg.StripeMode
	if mode == "" {
		mode = "test"
	}

	if mode == "live" && !strings.HasPrefix(cfg.StripeSecretKey, "sk_live_") {
		return fmt.Errorf("STRIPE_MODE=live but STRIPE_SECRET_KEY does not start with sk_live_")
	}
	if mode == "test" && !strings.HasPrefix(cfg.StripeSecretKey, "sk_test_") {
		return fmt.Errorf("STRIPE_MODE=test but STRIPE_SECRET_KEY does not start with sk_test_")
	}

	stripe.Key = cfg.StripeSecretKey

	slog.Info("billing: Stripe initialized",
		"mode", mode,
	)
	return nil
}
