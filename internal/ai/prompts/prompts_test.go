package prompts

import (
	"testing"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/ai/rag"
)

func TestPromptBuilder_Build_Categorize(t *testing.T) {
	t.Parallel()
	pb := &PromptBuilder{versioner: NewPromptVersioner(nil)}

	email := EmailInput{
		Subject:     "Invoice #1234 Due Tomorrow",
		FromAddress: "billing@vendor.com",
		BodyText:    "Please find attached invoice #1234 for $5,000 due tomorrow.",
	}

	messages, version, err := pb.Build(ai.TaskCategorize, email, nil, nil, "", uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Errorf("expected default version 0, got %d", version)
	}
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages (system + user), got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("first message should be system, got %s", messages[0].Role)
	}
}

func TestPromptBuilder_Build_WithContext(t *testing.T) {
	t.Parallel()
	pb := &PromptBuilder{versioner: NewPromptVersioner(nil)}

	email := EmailInput{Subject: "Meeting", FromAddress: "boss@company.com", BodyText: "Let's meet."}
	ctx := []rag.ContextItem{
		{SourceType: "sent_email", ContentPreview: "Previous reply about meeting", Similarity: 0.85},
	}

	messages, _, err := pb.Build(ai.TaskDraftReply, email, ctx, nil, "", uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	// Should have system + user (with context + input)
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}
}

func TestPromptBuilder_Build_WithFewShot(t *testing.T) {
	t.Parallel()
	pb := &PromptBuilder{versioner: NewPromptVersioner(nil)}

	email := EmailInput{Subject: "Hello", FromAddress: "friend@example.com", BodyText: "How are you?"}
	config := &UserPromptConfig{
		PersonalitySummary: "Formal and brief",
		FewShotExamples: []FewShotExample{
			{Input: "How are you?", Output: "I'm well, thank you."},
		},
	}

	messages, _, err := pb.Build(ai.TaskDraftReply, email, nil, config, "", uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	// Should have system + few_shot + user
	if len(messages) < 3 {
		t.Fatalf("expected at least 3 messages (system + few_shot + user), got %d", len(messages))
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input  string
		expect int
	}{
		{"", 0},
		{"test", 1},
		{"hello world test", 4},
	}
	for _, tt := range tests {
		result := EstimateTokens(tt.input)
		if result != tt.expect {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, result, tt.expect)
		}
	}
}

func TestEnforceBudget_TruncatesFewShotFirst(t *testing.T) {
	t.Parallel()
	longContent := make([]byte, 2000)
	for i := range longContent {
		longContent[i] = 'x'
	}

	sections := []PromptSection{
		{Name: "system", Content: "system prompt", Priority: 4, MaxTokens: BudgetSystem},
		{Name: "few_shot", Content: string(longContent), Priority: 1, MaxTokens: BudgetFewShot},
		{Name: "input", Content: "email content", Priority: 4, MaxTokens: 0},
	}

	result := EnforceBudget(sections)

	for _, s := range result {
		if s.Name == "few_shot" {
			tokens := EstimateTokens(s.Content)
			if tokens > BudgetFewShot {
				t.Errorf("few_shot tokens %d exceeds budget %d", tokens, BudgetFewShot)
			}
		}
	}
}

func TestUserHash_Consistent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	h1 := userHash(id)
	h2 := userHash(id)
	if h1 != h2 {
		t.Errorf("userHash not consistent: %d != %d", h1, h2)
	}
}

func TestUserHash_DifferentForDifferentUsers(t *testing.T) {
	t.Parallel()
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	if userHash(id1) == userHash(id2) {
		t.Error("different users should usually have different hashes")
	}
}

func TestFormatEmailInput_Truncation(t *testing.T) {
	t.Parallel()
	longBody := make([]byte, 3000)
	for i := range longBody {
		longBody[i] = 'a'
	}
	email := EmailInput{Subject: "Test", FromAddress: "test@test.com", BodyText: string(longBody)}

	result := formatEmailInput(email, ai.TaskCategorize)
	// Body should be truncated to 500
	if len(result) > 600 { // 500 body + headers
		t.Errorf("categorize input too long: %d chars", len(result))
	}
}
