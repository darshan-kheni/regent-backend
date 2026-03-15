package realtime

import "strings"

// RealtimeStrategy identifies the method used for real-time email detection.
type RealtimeStrategy string

const (
	// StrategyIDLE uses IMAP IDLE (RFC 2177) for server-push notifications.
	StrategyIDLE RealtimeStrategy = "idle"
	// StrategyPoll uses periodic NOOP polling as a fallback.
	StrategyPoll RealtimeStrategy = "poll"
	// StrategyGmailPush uses Gmail Pub/Sub push notifications.
	StrategyGmailPush RealtimeStrategy = "gmail_push"
)

// DetectStrategy determines the best real-time strategy for a provider.
// Gmail accounts use Pub/Sub push. IMAP servers advertising IDLE capability
// use IDLE. Everything else falls back to NOOP polling every 2 minutes.
func DetectStrategy(provider string, capabilities []string) RealtimeStrategy {
	if strings.EqualFold(provider, "gmail") {
		return StrategyGmailPush
	}
	for _, cap := range capabilities {
		if strings.EqualFold(cap, "IDLE") {
			return StrategyIDLE
		}
	}
	return StrategyPoll
}
