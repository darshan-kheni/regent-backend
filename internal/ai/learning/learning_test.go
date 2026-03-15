package learning

import (
	"encoding/json"
	"testing"
)

func TestPreferenceSignal_Types(t *testing.T) {
	t.Parallel()
	validTypes := []string{"recategorize", "summary_edit", "reply_edit", "priority_override", "vip_assign"}
	for _, st := range validTypes {
		sig := PreferenceSignal{SignalType: st}
		if sig.SignalType != st {
			t.Errorf("signal type should be %s", st)
		}
	}
}

func TestSynthesisResult_JSON(t *testing.T) {
	t.Parallel()
	result := SynthesisResult{
		PersonalitySummary: "Formal, concise communicator",
		FewShotExamples: []FewShotPair{
			{Input: "Meeting request", Output: "Dear colleague, I'd be happy to meet..."},
		},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var parsed SynthesisResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.PersonalitySummary != result.PersonalitySummary {
		t.Error("personality summary mismatch")
	}
	if len(parsed.FewShotExamples) != 1 {
		t.Error("expected 1 few-shot example")
	}
}

func TestFewShotPair_JSON(t *testing.T) {
	t.Parallel()
	pair := FewShotPair{Input: "question", Output: "answer"}
	data, err := json.Marshal(pair)
	if err != nil {
		t.Fatal(err)
	}
	var parsed FewShotPair
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Input != "question" || parsed.Output != "answer" {
		t.Error("pair mismatch")
	}
}
