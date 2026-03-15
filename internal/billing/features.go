package billing

// PlanFeatures is the canonical feature matrix mapping plan names to their features.
// This is the source of truth used by feature gating middleware.
var PlanFeatures = map[string][]string{
	PlanFree: {
		"email_fetch", "email_send", "categorize", "prioritize",
	},
	PlanAttache: {
		"email_fetch", "email_send", "categorize", "prioritize",
		"summarize", "draft_reply", "tone_analysis", "rag",
		"push", "email_digest", "basic_behavior",
		"calendar_sync",
		"task_extraction",
	},
	PlanPrivyCouncil: {
		"email_fetch", "email_send", "categorize", "prioritize",
		"summarize", "draft_reply", "tone_analysis", "rag",
		"push", "email_digest", "basic_behavior",
		"premium_draft", "all_channels", "full_behavior",
		"wellness", "rules_unlimited", "briefs",
		"calendar_sync", "scheduling_assist", "meeting_prep",
		"task_extraction", "task_delegation",
	},
	PlanEstate: {
		"email_fetch", "email_send", "categorize", "prioritize",
		"summarize", "draft_reply", "tone_analysis", "rag",
		"push", "email_digest", "basic_behavior",
		"premium_draft", "all_channels", "full_behavior",
		"wellness", "rules_unlimited", "briefs",
		"calendar_sync", "scheduling_assist", "meeting_prep",
		"task_extraction", "task_delegation",
		"custom_models", "dedicated_channels", "coaching",
		"quarterly", "unlimited_everything",
	},
}

// planFeatureIndex is a precomputed lookup for O(1) feature checks.
var planFeatureIndex map[string]map[string]bool

func init() {
	planFeatureIndex = make(map[string]map[string]bool, len(PlanFeatures))
	for plan, features := range PlanFeatures {
		idx := make(map[string]bool, len(features))
		for _, f := range features {
			idx[f] = true
		}
		planFeatureIndex[plan] = idx
	}
}

// HasFeature returns true if the given plan includes the specified feature.
func HasFeature(plan, feature string) bool {
	idx, ok := planFeatureIndex[plan]
	if !ok {
		return false
	}
	return idx[feature]
}

// SuggestUpgrade returns the name of the lowest-tier plan that includes the
// given feature. Returns an empty string if no plan has the feature.
func SuggestUpgrade(currentPlan, feature string) string {
	// Check plans in order from lowest to highest.
	ordered := []string{PlanFree, PlanAttache, PlanPrivyCouncil, PlanEstate}
	currentOrder := planOrder[currentPlan]

	for _, plan := range ordered {
		if planOrder[plan] <= currentOrder {
			continue
		}
		if HasFeature(plan, feature) {
			return plan
		}
	}
	return ""
}

// FeatureLabel returns a human-readable label for a feature key.
func FeatureLabel(feature string) string {
	labels := map[string]string{
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
		"calendar_sync":        "Calendar Sync",
		"scheduling_assist":    "AI Scheduling Assistant",
		"meeting_prep":         "Meeting Prep Briefs",
		"task_extraction":      "Task Extraction & Board",
		"task_delegation":      "Task Delegation",
	}
	if label, ok := labels[feature]; ok {
		return label
	}
	return feature
}
