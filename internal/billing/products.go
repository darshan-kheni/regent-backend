package billing

import "fmt"

// ResolvePriceIDToPlan maps a Stripe price ID to the corresponding plan name.
func ResolvePriceIDToPlan(priceID string) (string, error) {
	plan, ok := GetPlanByPriceID(priceID)
	if !ok {
		return "", fmt.Errorf("unknown price ID: %s", priceID)
	}
	return plan.Name, nil
}

// GetPlanFeatureList returns human-readable feature descriptions for a plan.
func GetPlanFeatureList(planName string) []string {
	featureLabels := map[string]string{
		"email_fetch":          "Email fetching",
		"email_send":           "Email sending",
		"categorize":           "AI categorization",
		"prioritize":           "Priority scoring",
		"summarize":            "Email summaries",
		"draft_reply":          "AI draft replies",
		"tone_analysis":        "Tone analysis",
		"rag":                  "RAG context retrieval",
		"push":                 "Push notifications",
		"email_digest":         "Email digests",
		"basic_behavior":       "Basic behavior intelligence",
		"premium_draft":        "Premium AI drafts",
		"all_channels":         "All notification channels",
		"full_behavior":        "Full behavior intelligence",
		"wellness":             "Wellness reports",
		"rules_unlimited":      "Unlimited AI rules",
		"briefs":               "Context briefs",
		"custom_models":        "Custom AI models",
		"dedicated_channels":   "Dedicated channels",
		"coaching":             "AI coaching",
		"quarterly":            "Quarterly reports",
		"unlimited_everything": "Unlimited everything",
	}

	plan, ok := GetPlanByName(planName)
	if !ok {
		return nil
	}

	labels := make([]string, 0, len(plan.Features))
	for _, f := range plan.Features {
		if label, ok := featureLabels[f]; ok {
			labels = append(labels, label)
		} else {
			labels = append(labels, f)
		}
	}
	return labels
}
