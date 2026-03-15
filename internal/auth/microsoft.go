package auth

import "fmt"

const (
	// MicrosoftIMAPScope is the Outlook IMAP access scope.
	MicrosoftIMAPScope = "https://outlook.office365.com/IMAP.AccessAsUser.All"
	// MicrosoftOfflineScope requests a refresh token for offline access.
	MicrosoftOfflineScope = "offline_access"
)

// ValidateMicrosoftScopes checks that the Outlook IMAP scope was granted.
func ValidateMicrosoftScopes(grantedScopes []string) error {
	for _, s := range grantedScopes {
		if s == MicrosoftIMAPScope {
			return nil
		}
	}
	return fmt.Errorf("Outlook IMAP scope not granted: required %s", MicrosoftIMAPScope)
}
