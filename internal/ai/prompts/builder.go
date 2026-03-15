package prompts

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/rag"
)

// FewShotExample represents a user-specific example for prompt injection.
type FewShotExample struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// UserPromptConfig holds user-specific prompt customization from preference learning.
type UserPromptConfig struct {
	PersonalitySummary string           `json:"personality_summary"`
	FewShotExamples    []FewShotExample `json:"few_shot_examples"`
}

// PromptBuilder assembles AI prompts from templates, context, and user config.
type PromptBuilder struct {
	pool      *pgxpool.Pool
	versioner *PromptVersioner
}

// NewPromptBuilder creates a PromptBuilder.
func NewPromptBuilder(pool *pgxpool.Pool) *PromptBuilder {
	return &PromptBuilder{
		pool:      pool,
		versioner: NewPromptVersioner(pool),
	}
}

// EmailInput is a simplified view of an email for template rendering.
type EmailInput struct {
	Subject        string
	FromAddress    string
	FromName       string
	BodyText       string
	HasAttachments bool
}

// TemplateData is the data passed to Go text/template rendering.
type TemplateData struct {
	Email         EmailInput
	Context       []rag.ContextItem
	FewShot       []FewShotExample
	Personality   string
	MemoryContext string // Injected from AI Memory system
}

// Build assembles a prompt for the given task, applying token budget constraints.
func (pb *PromptBuilder) Build(task ai.TaskType, email EmailInput, context []rag.ContextItem, userConfig *UserPromptConfig, memoryContext string, userID uuid.UUID) ([]ai.Message, int, error) {
	// Get template (versioned with A/B testing)
	tmplText, version, err := pb.versioner.GetActiveVersion(nil, task, userID)
	if err != nil || tmplText == "" {
		tmplText = getDefaultTemplate(task)
		version = 0
	}

	// Build template data
	data := TemplateData{
		Email:   email,
		Context: context,
	}
	if userConfig != nil {
		data.FewShot = userConfig.FewShotExamples
		data.Personality = userConfig.PersonalitySummary
	}
	data.MemoryContext = memoryContext

	// Render system prompt
	systemPrompt, err := renderTemplate(tmplText, data)
	if err != nil {
		return nil, 0, fmt.Errorf("rendering prompt template: %w", err)
	}

	// Build sections with token budgets
	sections := []PromptSection{
		{Name: "system", Content: systemPrompt, Priority: 4, MaxTokens: BudgetSystem},
	}

	// Context section from RAG
	if len(context) > 0 {
		var ctxParts []string
		for _, c := range context {
			ctxParts = append(ctxParts, fmt.Sprintf("[%s] %s (%.0f%% match)", c.SourceType, c.ContentPreview, c.Similarity*100))
		}
		sections = append(sections, PromptSection{
			Name:      "context",
			Content:   strings.Join(ctxParts, "\n"),
			Priority:  2,
			MaxTokens: BudgetContext,
		})
	}

	// Memory context section
	if memoryContext != "" {
		sections = append(sections, PromptSection{
			Name:      "memory",
			Content:   memoryContext,
			Priority:  3,
			MaxTokens: BudgetContext, // shares context budget
		})
	}

	// Few-shot examples
	if userConfig != nil && len(userConfig.FewShotExamples) > 0 {
		var fsParts []string
		for _, ex := range userConfig.FewShotExamples {
			fsParts = append(fsParts, fmt.Sprintf("Input: %s\nOutput: %s", ex.Input, ex.Output))
		}
		sections = append(sections, PromptSection{
			Name:      "few_shot",
			Content:   strings.Join(fsParts, "\n---\n"),
			Priority:  1,
			MaxTokens: BudgetFewShot,
		})
	}

	// Input section (email content)
	inputText := formatEmailInput(email, task)
	sections = append(sections, PromptSection{
		Name:      "input",
		Content:   inputText,
		Priority:  4, // Never truncate input
		MaxTokens: 0, // Variable
	})

	// Enforce budget
	sections = EnforceBudget(sections)

	// Assemble messages
	messages := assembleMessages(sections)

	return messages, version, nil
}

// formatEmailInput creates the email portion of the prompt with the full body.
func formatEmailInput(email EmailInput, _ ai.TaskType) string {
	return fmt.Sprintf("From: %s <%s>\nSubject: %s\nHas Attachments: %v\n\n%s",
		email.FromName, email.FromAddress, email.Subject, email.HasAttachments, email.BodyText)
}

// assembleMessages converts sections into ai.Message slice.
func assembleMessages(sections []PromptSection) []ai.Message {
	var messages []ai.Message

	// System message combines system + memory
	var systemParts []string
	for _, s := range sections {
		if s.Name == "system" || s.Name == "memory" {
			systemParts = append(systemParts, s.Content)
		}
	}
	if len(systemParts) > 0 {
		messages = append(messages, ai.Message{Role: "system", Content: strings.Join(systemParts, "\n\n")})
	}

	// Few-shot as alternating user/assistant messages
	for _, s := range sections {
		if s.Name == "few_shot" {
			messages = append(messages, ai.Message{Role: "user", Content: "Here are examples of my preferred style:\n" + s.Content})
		}
	}

	// Context + Input as user message
	var userParts []string
	for _, s := range sections {
		if s.Name == "context" {
			userParts = append(userParts, "Relevant context:\n"+s.Content)
		}
		if s.Name == "input" {
			userParts = append(userParts, s.Content)
		}
	}
	if len(userParts) > 0 {
		messages = append(messages, ai.Message{Role: "user", Content: strings.Join(userParts, "\n\n")})
	}

	return messages
}

func renderTemplate(tmplText string, data TemplateData) (string, error) {
	tmpl, err := template.New("prompt").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}
