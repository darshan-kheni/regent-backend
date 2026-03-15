package ai

import "time"

// TaskType identifies the kind of AI processing task.
type TaskType string

const (
	TaskCategorize       TaskType = "categorize"
	TaskPrioritize       TaskType = "prioritize"
	TaskSummarize        TaskType = "summarize"
	TaskDraftReply       TaskType = "draft_reply"
	TaskPremiumDraft     TaskType = "premium_draft"
	TaskEmbedding        TaskType = "embedding"
	TaskPreferenceSynth  TaskType = "preference_synthesis"
	TaskBehaviorAnalysis TaskType = "behavior_analysis"
)

// ModelConfig specifies how to invoke a particular model for a task.
type ModelConfig struct {
	ModelID     string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
	CostPer1K   float64 // shadow cost per 1K tokens
	Tier        string  // "fast", "mid", "quality", "premium"
}

// DefaultModels maps each task type to its default model configuration.
var DefaultModels = map[TaskType]ModelConfig{
	TaskCategorize:       {ModelID: "gemma3:4b", Temperature: 0.1, MaxTokens: 300, Timeout: 30 * time.Second, CostPer1K: 0.0001, Tier: "fast"},
	TaskPrioritize:       {ModelID: "gemma3:4b", Temperature: 0.1, MaxTokens: 300, Timeout: 30 * time.Second, CostPer1K: 0.0001, Tier: "fast"},
	TaskSummarize:        {ModelID: "ministral-3:8b", Temperature: 0.3, MaxTokens: 300, Timeout: 45 * time.Second, CostPer1K: 0.0003, Tier: "mid"},
	TaskDraftReply:       {ModelID: "gemma3:12b", Temperature: 0.6, MaxTokens: 500, Timeout: 60 * time.Second, CostPer1K: 0.0005, Tier: "quality"},
	TaskPremiumDraft:     {ModelID: "gpt-oss:120b", Temperature: 0.4, MaxTokens: 800, Timeout: 90 * time.Second, CostPer1K: 0.003, Tier: "premium"},
	TaskEmbedding:        {ModelID: "nomic-embed-text", Temperature: 0, MaxTokens: 0, Timeout: 30 * time.Second, CostPer1K: 0.00005, Tier: "fast"},
	TaskPreferenceSynth:  {ModelID: "gpt-oss:120b", Temperature: 0.3, MaxTokens: 1000, Timeout: 90 * time.Second, CostPer1K: 0.003, Tier: "premium"},
	TaskBehaviorAnalysis: {ModelID: "ministral-3:8b", Temperature: 0.3, MaxTokens: 500, Timeout: 45 * time.Second, CostPer1K: 0.0003, Tier: "mid"},
}

// EmailMeta provides email context for routing decisions.
type EmailMeta struct {
	Priority             int
	Category             string
	SenderIsVIP          bool
	UserRequestedPremium bool
}
