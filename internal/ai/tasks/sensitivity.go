package tasks

import (
	"strings"

	"github.com/darshan-kheni/regent/internal/models"
)

// SensitivityScorer determines if an email requires premium model treatment.
type SensitivityScorer struct{}

func NewSensitivityScorer() *SensitivityScorer {
	return &SensitivityScorer{}
}

// ShouldUpgrade returns true if the email warrants premium AI processing.
func (s *SensitivityScorer) ShouldUpgrade(email models.Email, catResult *CategorizeResult) bool {
	// High priority
	if catResult != nil && catResult.PriorityScore > 85 {
		return true
	}

	// Legal or Finance category
	if catResult != nil {
		cat := catResult.PrimaryCategory
		if cat == "Legal" || cat == "Finance" {
			return true
		}
	}

	// Sensitive keywords in body or subject
	combined := strings.ToLower(email.Subject + " " + email.BodyText)

	// Condolence keywords
	condolence := []string{"sorry for your loss", "condolences", "sympathy", "passed away", "funeral"}
	for _, kw := range condolence {
		if strings.Contains(combined, kw) {
			return true
		}
	}

	// Legal terms
	legal := []string{"attorney", "lawsuit", "subpoena", "litigation", "legal action", "court order"}
	for _, kw := range legal {
		if strings.Contains(combined, kw) {
			return true
		}
	}

	// Negotiation terms
	negotiation := []string{"counteroffer", "negotiate", "terms and conditions", "offer letter", "salary negotiation"}
	for _, kw := range negotiation {
		if strings.Contains(combined, kw) {
			return true
		}
	}

	return false
}
