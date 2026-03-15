package ai

import "log/slog"

// ModelRouter selects the optimal model configuration for a given task type,
// with auto-upgrade logic for sensitive emails.
type ModelRouter struct {
	configs map[TaskType]ModelConfig
}

// NewModelRouter creates a ModelRouter with default model configurations.
func NewModelRouter() *ModelRouter {
	return &ModelRouter{configs: DefaultModels}
}

// Route returns the ModelConfig for a task, potentially upgrading to premium
// for sensitive or high-priority emails.
func (r *ModelRouter) Route(task TaskType, meta EmailMeta) ModelConfig {
	cfg, ok := r.configs[task]
	if !ok {
		slog.Warn("unknown task type, defaulting to categorize", "task", string(task))
		cfg = r.configs[TaskCategorize]
	}

	if task == TaskDraftReply && r.shouldUpgrade(meta) {
		slog.Debug("auto-upgrading to premium model",
			"task", string(task),
			"priority", meta.Priority,
			"category", meta.Category,
			"vip", meta.SenderIsVIP,
		)
		cfg = r.configs[TaskPremiumDraft]
	}

	return cfg
}

// shouldUpgrade determines if an email warrants premium model treatment.
func (r *ModelRouter) shouldUpgrade(meta EmailMeta) bool {
	if meta.Priority > 85 {
		return true
	}
	if meta.Category == "Legal" || meta.Category == "Finance" {
		return true
	}
	if meta.SenderIsVIP {
		return true
	}
	if meta.UserRequestedPremium {
		return true
	}
	return false
}

// GetConfig returns the raw config for a task without upgrade logic.
func (r *ModelRouter) GetConfig(task TaskType) (ModelConfig, bool) {
	cfg, ok := r.configs[task]
	return cfg, ok
}
