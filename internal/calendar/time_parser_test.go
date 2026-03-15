package calendar

import (
	"testing"
	"time"
)

func TestParseSchedulingTime(t *testing.T) {
	ref := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC) // Sunday March 8, 2026

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result *DateTimeResult)
	}{
		{
			name:  "morning period",
			input: "morning",
			check: func(t *testing.T, r *DateTimeResult) {
				if r.Start.Hour() != 9 {
					t.Errorf("expected hour 9, got %d", r.Start.Hour())
				}
				if r.End == nil {
					t.Fatal("expected end time for range")
				}
				if r.End.Hour() != 12 {
					t.Errorf("expected end hour 12, got %d", r.End.Hour())
				}
				if !r.HasTime {
					t.Error("expected HasTime to be true")
				}
			},
		},
		{
			name:  "afternoon period",
			input: "afternoon",
			check: func(t *testing.T, r *DateTimeResult) {
				if r.Start.Hour() != 13 {
					t.Errorf("expected hour 13, got %d", r.Start.Hour())
				}
				if r.End == nil || r.End.Hour() != 17 {
					t.Errorf("expected end hour 17")
				}
			},
		},
		{
			name:  "time range with PM",
			input: "between 2-4 PM",
			check: func(t *testing.T, r *DateTimeResult) {
				if r.Start.Hour() != 14 {
					t.Errorf("expected start hour 14, got %d", r.Start.Hour())
				}
				if r.End == nil || r.End.Hour() != 16 {
					t.Errorf("expected end hour 16")
				}
			},
		},
		{
			name:  "relative date - next Tuesday",
			input: "next Tuesday",
			check: func(t *testing.T, r *DateTimeResult) {
				if r.Start.Weekday() != time.Tuesday {
					t.Errorf("expected Tuesday, got %s", r.Start.Weekday())
				}
				if !r.Start.After(ref) {
					t.Error("expected future date")
				}
			},
		},
		{
			name:  "explicit date",
			input: "March 15 2026",
			check: func(t *testing.T, r *DateTimeResult) {
				if r.Start.Month() != time.March || r.Start.Day() != 15 {
					t.Errorf("expected March 15, got %s %d", r.Start.Month(), r.Start.Day())
				}
			},
		},
		{
			name:    "unparseable",
			input:   "asdfghjkl",
			wantErr: true,
		},
		{
			name:    "empty handled",
			input:   "",
			wantErr: true,
		},
		{
			name:  "explicit with time",
			input: "March 15 at 3pm",
			check: func(t *testing.T, r *DateTimeResult) {
				if !r.HasTime {
					t.Error("expected HasTime to be true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSchedulingTime(tt.input, ref)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
