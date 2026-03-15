package config

import (
	"encoding/base64"
	"fmt"
)

// validate checks all required configuration values at startup.
// Panics with a clear message if any required value is missing.
func validate(cfg *Config) {
	if cfg.Database.URL == "" {
		panic("DATABASE_URL is required but not set")
	}

	if cfg.Environment == "production" {
		if cfg.Auth.JWTSecret == "" {
			panic("JWT_SECRET is required in production")
		}
		if cfg.Auth.Mode == "stub" {
			panic("AUTH_MODE=stub is not allowed in production")
		}
		if cfg.Auth.SupabaseServiceKey == "" {
			panic("SUPABASE_SERVICE_KEY is required in production")
		}
		if cfg.Auth.Mode == "jwt" && cfg.Auth.SupabaseURL == "" {
			panic("SUPABASE_URL is required when AUTH_MODE=jwt")
		}
	}

	if cfg.Auth.EncryptionMasterKey != "" {
		keyBytes, err := base64.StdEncoding.DecodeString(cfg.Auth.EncryptionMasterKey)
		if err != nil {
			panic("ENCRYPTION_MASTER_KEY must be valid base64: " + err.Error())
		}
		if len(keyBytes) != 32 {
			panic(fmt.Sprintf("ENCRYPTION_MASTER_KEY must decode to 32 bytes, got %d", len(keyBytes)))
		}
	}

	// Stripe billing validation
	if cfg.Environment == "production" {
		if cfg.Billing.StripeMode == "live" && cfg.Billing.StripeSecretKey != "" {
			if len(cfg.Billing.StripeSecretKey) < 10 || cfg.Billing.StripeSecretKey[:8] != "sk_live_" {
				panic("STRIPE_SECRET_KEY must start with sk_live_ when STRIPE_MODE=live")
			}
		}
		if cfg.Billing.StripeWebhookSecret == "" {
			fmt.Println("WARNING: STRIPE_WEBHOOK_SECRET is not set — webhook signature verification disabled")
		}
	}

	validEnvs := map[string]bool{"development": true, "staging": true, "production": true}
	if !validEnvs[cfg.Environment] {
		panic(fmt.Sprintf("ENVIRONMENT must be development, staging, or production; got %q", cfg.Environment))
	}
}
