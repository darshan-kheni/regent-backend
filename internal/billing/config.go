package billing

// PlanLimits defines resource limits per plan.
type PlanLimits struct {
	MaxAccounts    int   `json:"max_accounts"`    // 0 = unlimited
	DailyTokens    int64 `json:"daily_tokens"`    // 0 = unlimited
	EmailsPerMonth int64 `json:"emails_month"`    // 0 = unlimited
}

// PlanTier defines a billing plan with its pricing, limits, and features.
type PlanTier struct {
	Name       string    `json:"name"`
	PriceID    string    `json:"price_id"`
	PriceCents int       `json:"price_cents"`
	Limits     PlanLimits `json:"limits"`
	Features   []string  `json:"features"`
}

// Plan name constants.
const (
	PlanFree         = "free"
	PlanAttache      = "attache"
	PlanPrivyCouncil = "privy_council"
	PlanEstate       = "estate"
)

// planOrder defines the hierarchy for plan comparison.
var planOrder = map[string]int{
	PlanFree:         0,
	PlanAttache:      1,
	PlanPrivyCouncil: 2,
	PlanEstate:       3,
}

// PlanAtLeast returns true if the current plan meets or exceeds the minimum plan.
func PlanAtLeast(current, minimum string) bool {
	return planOrder[current] >= planOrder[minimum]
}

// GetPlanOrder returns the plan ordering map.
func GetPlanOrder() map[string]int {
	return planOrder
}

// plans stores the canonical plan definitions. Populated at init from config.
var plans []PlanTier

// InitPlans initializes plan definitions with Stripe price IDs from config.
func InitPlans(cfg BillingConfig) {
	plans = []PlanTier{
		{
			Name:       PlanFree,
			PriceID:    cfg.StripePriceFree,
			PriceCents: 0,
			Limits:     PlanLimits{MaxAccounts: 2, DailyTokens: 50_000, EmailsPerMonth: 500},
			Features:   []string{"email_fetch", "email_send", "categorize", "prioritize"},
		},
		{
			Name:       PlanAttache,
			PriceID:    cfg.StripePriceAttache,
			PriceCents: 9700,
			Limits:     PlanLimits{MaxAccounts: 10, DailyTokens: 500_000, EmailsPerMonth: 10_000},
			Features:   []string{"email_fetch", "email_send", "categorize", "prioritize", "summarize", "draft_reply", "tone_analysis", "rag", "push", "email_digest", "basic_behavior"},
		},
		{
			Name:       PlanPrivyCouncil,
			PriceID:    cfg.StripePricePrivy,
			PriceCents: 29700,
			Limits:     PlanLimits{MaxAccounts: 25, DailyTokens: 2_000_000, EmailsPerMonth: 50_000},
			Features:   []string{"email_fetch", "email_send", "categorize", "prioritize", "summarize", "draft_reply", "tone_analysis", "rag", "push", "email_digest", "basic_behavior", "premium_draft", "all_channels", "full_behavior", "wellness", "rules_unlimited", "briefs"},
		},
		{
			Name:       PlanEstate,
			PriceID:    cfg.StripePriceEstate,
			PriceCents: 69700,
			Limits:     PlanLimits{MaxAccounts: 0, DailyTokens: 0, EmailsPerMonth: 0}, // 0 = unlimited
			Features:   []string{"email_fetch", "email_send", "categorize", "prioritize", "summarize", "draft_reply", "tone_analysis", "rag", "push", "email_digest", "basic_behavior", "premium_draft", "all_channels", "full_behavior", "wellness", "rules_unlimited", "briefs", "custom_models", "dedicated_channels", "coaching", "quarterly", "unlimited_everything"},
		},
	}
}

// GetPlanByName returns the plan definition for the given name.
func GetPlanByName(name string) (PlanTier, bool) {
	for _, p := range plans {
		if p.Name == name {
			return p, true
		}
	}
	return PlanTier{}, false
}

// GetPlanByPriceID returns the plan definition matching a Stripe price ID.
func GetPlanByPriceID(priceID string) (PlanTier, bool) {
	for _, p := range plans {
		if p.PriceID == priceID && p.PriceID != "" {
			return p, true
		}
	}
	return PlanTier{}, false
}

// GetAllPlans returns all plan definitions.
func GetAllPlans() []PlanTier {
	return plans
}

// BillingConfig holds Stripe-related configuration.
type BillingConfig struct {
	StripeSecretKey      string
	StripePublishableKey string
	StripeWebhookSecret  string
	StripeMode           string // "test" or "live"
	StripePriceFree      string
	StripePriceAttache   string
	StripePricePrivy     string
	StripePriceEstate    string
	FrontendURL          string
}
