package auth

import "fmt"

const (
	// MicrosoftIMAPScope is the Outlook IMAP access scope.
	MicrosoftIMAPScope = "https://outlook.office365.com/IMAP.AccessAsUser.All"
	// MicrosoftOfflineScope requests a refresh token for offline access.
	MicrosoftOfflineScope = "offline_access"
)

// ValidateMicrosoftScopes checks that sufficient email access scopes were granted.
func ValidateMicrosoftScopes(grantedScopes []string) error {
	accepted := map[string]bool{
		MicrosoftIMAPScope:      true,
		"IMAP.AccessAsUser.All": true,
		"Mail.Read":             true,
		"Mail.Send":             true,
		"mail.read":             true,
		"mail.send":             true,
	}
	for _, s := range grantedScopes {
		if accepted[s] {
			return nil
		}
	}
	return fmt.Errorf("insufficient email scopes: need Mail.Read, Mail.Send, or IMAP access")
}
