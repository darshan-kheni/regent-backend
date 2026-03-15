package imap

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestFetchMessages_EmptyUIDs(t *testing.T) {
	t.Parallel()

	// FetchMessages with empty UIDs should return nil, nil without touching the client.
	results, err := FetchMessages(nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got: %v", results)
	}

	// Also test with an empty slice (not nil).
	results, err = FetchMessages(nil, []imap.UID{})
	if err != nil {
		t.Fatalf("expected no error for empty slice, got: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty slice, got: %v", results)
	}
}

func TestBatchCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		total    int
		expected int
	}{
		{"zero UIDs", 0, 0},
		{"negative UIDs", -5, 0},
		{"one UID", 1, 1},
		{"exactly 49", 49, 1},
		{"exactly 50", 50, 1},
		{"51 UIDs", 51, 2},
		{"100 UIDs", 100, 2},
		{"150 UIDs", 150, 3},
		{"151 UIDs", 151, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BatchCount(tt.total)
			if got != tt.expected {
				t.Errorf("BatchCount(%d) = %d, want %d", tt.total, got, tt.expected)
			}
		})
	}
}

func TestBatchSplitting(t *testing.T) {
	t.Parallel()

	// Verify that the batching logic in FetchMessages would create
	// the correct number of batches for various UID counts.
	tests := []struct {
		name          string
		uidCount      int
		expectedBatch int
	}{
		{"0 UIDs", 0, 0},
		{"1 UID", 1, 1},
		{"49 UIDs", 49, 1},
		{"50 UIDs", 50, 1},
		{"51 UIDs", 51, 2},
		{"100 UIDs", 100, 2},
		{"150 UIDs", 150, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			batchCount := 0
			for i := 0; i < tt.uidCount; i += batchSize {
				batchCount++
				end := min(i+batchSize, tt.uidCount)

				// Verify batch boundaries
				batchLen := end - i
				if batchLen > batchSize {
					t.Errorf("batch too large: got %d, max %d", batchLen, batchSize)
				}
				if batchLen <= 0 {
					t.Errorf("empty batch at offset %d", i)
				}
			}

			if batchCount != tt.expectedBatch {
				t.Errorf("expected %d batches, got %d", tt.expectedBatch, batchCount)
			}
		})
	}
}

func TestXOAuth2Client_Start(t *testing.T) {
	t.Parallel()

	client := &xoauth2Client{
		username:    "user@example.com",
		accessToken: "ya29.test-token-123",
	}

	mech, ir, err := client.Start()
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	if mech != "XOAUTH2" {
		t.Errorf("mechanism = %q, want %q", mech, "XOAUTH2")
	}

	expected := "user=user@example.com\x01auth=Bearer ya29.test-token-123\x01\x01"
	if string(ir) != expected {
		t.Errorf("initial response = %q, want %q", string(ir), expected)
	}
}

func TestXOAuth2Client_Next(t *testing.T) {
	t.Parallel()

	client := &xoauth2Client{
		username:    "user@example.com",
		accessToken: "token",
	}

	// On challenge, XOAUTH2 should return empty response.
	resp, err := client.Next([]byte("some-challenge"))
	if err != nil {
		t.Fatalf("Next() returned error: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty response, got %q", resp)
	}
}
