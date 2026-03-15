package metering

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestPlanQuotas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		plan           string
		dailyTokens    int64
		premiumMonthly int
	}{
		{"free", 50_000, 0},
		{"attache", 500_000, 20},
		{"privy_council", 2_000_000, 200},
		{"estate", 0, 0},
	}
	for _, tt := range tests {
		limits := PlanQuotas[tt.plan]
		if limits.DailyTokens != tt.dailyTokens {
			t.Errorf("%s daily tokens = %d, want %d", tt.plan, limits.DailyTokens, tt.dailyTokens)
		}
		if limits.PremiumMonthly != tt.premiumMonthly {
			t.Errorf("%s premium = %d, want %d", tt.plan, limits.PremiumMonthly, tt.premiumMonthly)
		}
	}
}

func TestAuditEntry_JSON(t *testing.T) {
	t.Parallel()
	entry := AuditEntry{
		UserID:    uuid.New(),
		TenantID:  uuid.New(),
		TaskType:  "categorize",
		ModelUsed: "qwen3:4b",
		TokensIn:  100,
		TokensOut: 50,
		LatencyMs: 200,
		CacheHit:  false,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}
}

func TestDailyUsage_Struct(t *testing.T) {
	t.Parallel()
	d := DailyUsage{TotalTokens: 1000, TotalCalls: 10, CacheHits: 3, PremiumCalls: 2}
	if d.TotalTokens != 1000 || d.TotalCalls != 10 {
		t.Error("struct fields not set correctly")
	}
}

func TestModelBreakdown_Struct(t *testing.T) {
	t.Parallel()
	m := ModelBreakdown{Model: "qwen3:4b", TotalTokens: 5000, TotalCalls: 50, AvgLatency: 100}
	if m.Model != "qwen3:4b" {
		t.Error("model not set correctly")
	}
}
