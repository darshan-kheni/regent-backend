package queue

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		plan     string
		expected int
	}{
		{"estate", 40},
		{"privy_council", 30},
		{"attache", 20},
		{"free", 10},
	}

	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			t.Parallel()
			got, ok := PlanPriority[tt.plan]
			require.True(t, ok, "plan %q should exist in PlanPriority", tt.plan)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPlanPriority_Ordering(t *testing.T) {
	t.Parallel()

	// Estate should always be highest, free should always be lowest
	assert.Greater(t, PlanPriority["estate"], PlanPriority["privy_council"])
	assert.Greater(t, PlanPriority["privy_council"], PlanPriority["attache"])
	assert.Greater(t, PlanPriority["attache"], PlanPriority["free"])
}

func TestPlanStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		plan           string
		expectedLen    int
		mustContain    []PipelineStage
		mustNotContain []PipelineStage
	}{
		{
			plan:           "free",
			expectedLen:    1,
			mustContain:    []PipelineStage{StageCategorizing},
			mustNotContain: []PipelineStage{StageSummarizing, StageDrafting},
		},
		{
			plan:        "attache",
			expectedLen: 3,
			mustContain: []PipelineStage{StageCategorizing, StageSummarizing, StageDrafting},
		},
		{
			plan:        "privy_council",
			expectedLen: 3,
			mustContain: []PipelineStage{StageCategorizing, StageSummarizing, StageDrafting},
		},
		{
			plan:        "estate",
			expectedLen: 3,
			mustContain: []PipelineStage{StageCategorizing, StageSummarizing, StageDrafting},
		},
	}

	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			t.Parallel()
			stages, ok := PlanStages[tt.plan]
			require.True(t, ok, "plan %q should exist in PlanStages", tt.plan)
			assert.Len(t, stages, tt.expectedLen)

			for _, s := range tt.mustContain {
				assert.True(t, ContainsStage(stages, s), "plan %q should contain stage %q", tt.plan, s)
			}
			for _, s := range tt.mustNotContain {
				assert.False(t, ContainsStage(stages, s), "plan %q should NOT contain stage %q", tt.plan, s)
			}
		})
	}
}

func TestContainsStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stages   []PipelineStage
		target   PipelineStage
		expected bool
	}{
		{
			name:     "found in list",
			stages:   []PipelineStage{StageCategorizing, StageSummarizing, StageDrafting},
			target:   StageSummarizing,
			expected: true,
		},
		{
			name:     "not found in list",
			stages:   []PipelineStage{StageCategorizing},
			target:   StageDrafting,
			expected: false,
		},
		{
			name:     "empty list",
			stages:   []PipelineStage{},
			target:   StageCategorizing,
			expected: false,
		},
		{
			name:     "nil list",
			stages:   nil,
			target:   StageComplete,
			expected: false,
		},
		{
			name:     "first element match",
			stages:   []PipelineStage{StageError, StageComplete},
			target:   StageError,
			expected: true,
		},
		{
			name:     "last element match",
			stages:   []PipelineStage{StageCategorizing, StageSummarizing, StageDrafting},
			target:   StageDrafting,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ContainsStage(tt.stages, tt.target)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestJobMarshal(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tenantID := uuid.New()
	emailID := uuid.New()

	job := Job{
		EmailID:  emailID,
		UserID:   userID,
		TenantID: tenantID,
		Plan:     "attache",
		Priority: PlanPriority["attache"],
	}

	assert.Equal(t, emailID, job.EmailID)
	assert.Equal(t, userID, job.UserID)
	assert.Equal(t, tenantID, job.TenantID)
	assert.Equal(t, "attache", job.Plan)
	assert.Equal(t, 20, job.Priority)
}

func TestPipelineStageConstants(t *testing.T) {
	t.Parallel()

	// Verify stage string values match the database CHECK constraint
	assert.Equal(t, PipelineStage("queued"), StageQueued)
	assert.Equal(t, PipelineStage("categorizing"), StageCategorizing)
	assert.Equal(t, PipelineStage("summarizing"), StageSummarizing)
	assert.Equal(t, PipelineStage("drafting"), StageDrafting)
	assert.Equal(t, PipelineStage("complete"), StageComplete)
	assert.Equal(t, PipelineStage("error"), StageError)
	assert.Equal(t, PipelineStage("skipped"), StageSkipped)
}

func TestAllPlansHaveStages(t *testing.T) {
	t.Parallel()

	// Every plan in PlanPriority should have a corresponding PlanStages entry
	for plan := range PlanPriority {
		stages, ok := PlanStages[plan]
		assert.True(t, ok, "plan %q has priority but no stages defined", plan)
		assert.NotEmpty(t, stages, "plan %q has empty stages", plan)
	}
}

func TestAllPlansHaveCategorization(t *testing.T) {
	t.Parallel()

	// Every plan must include categorization as the first stage
	for plan, stages := range PlanStages {
		require.NotEmpty(t, stages, "plan %q has no stages", plan)
		assert.Equal(t, StageCategorizing, stages[0],
			"plan %q first stage should be categorizing, got %q", plan, stages[0])
	}
}
