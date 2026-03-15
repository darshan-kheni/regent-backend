package ai

// PlanConfig defines what AI features are available for each billing plan.
type PlanConfig struct {
	Plan           string
	Categorize     bool
	Summarize      bool
	StandardDraft  bool
	PremiumDraft   bool
	AutoPremium    bool // auto-upgrade sensitive emails to premium
	PremiumDefault bool // always use premium model for drafts
}

// PlanConfigs maps plan names to their AI pipeline configuration.
var PlanConfigs = map[string]PlanConfig{
	"free":    {Plan: "free", Categorize: true},
	"attache": {Plan: "attache", Categorize: true, Summarize: true, StandardDraft: true},
	"privy_council": {Plan: "privy_council", Categorize: true, Summarize: true, StandardDraft: true, PremiumDraft: true, AutoPremium: true},
	"estate":  {Plan: "estate", Categorize: true, Summarize: true, StandardDraft: true, PremiumDraft: true, PremiumDefault: true},
}

// GetPlanConfig returns the AI configuration for the given plan.
func GetPlanConfig(plan string) PlanConfig {
	cfg, ok := PlanConfigs[plan]
	if !ok {
		return PlanConfigs["free"]
	}
	return cfg
}
