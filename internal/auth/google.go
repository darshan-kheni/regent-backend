package auth

import "fmt"

const (
	// GoogleGmailScope is the full Gmail access scope required for IMAP/API access.
	GoogleGmailScope = "https://mail.google.com/"
	// GoogleEmailScope requests the user's email address.
	GoogleEmailScope = "email"
	// GoogleProfileScope requests the user's basic profile.
	GoogleProfileScope = "profile"
)

// ValidateGoogleScopes checks that the Gmail scope was granted.
func ValidateGoogleScopes(grantedScopes []string) error {
	for _, s := range grantedScopes {
		if s == GoogleGmailScope {
			return nil
		}
	}
	return fmt.Errorf("Gmail scope not granted: required %s", GoogleGmailScope)
}
