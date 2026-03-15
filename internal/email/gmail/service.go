package gmail

import (
	"context"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Config holds Gmail API configuration.
type Config struct {
	ClientID      string
	ClientSecret  string
	PubSubTopic   string
	PubSubProject string
}

// NewGmailService constructs a Gmail API service with a persisting token source.
// The token source auto-refreshes and persists new tokens to the database.
func NewGmailService(ctx context.Context, cfg Config, token *oauth2.Token, persist func(*oauth2.Token)) (*gmail.Service, error) {
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{"https://mail.google.com/"},
	}

	ts := &persistingTokenSource{
		base:    oauthCfg.TokenSource(ctx, token),
		persist: persist,
	}

	return gmail.NewService(ctx, option.WithTokenSource(ts))
}

// persistingTokenSource wraps oauth2.TokenSource to persist refreshed tokens.
type persistingTokenSource struct {
	base    oauth2.TokenSource
	persist func(*oauth2.Token)
	mu      sync.Mutex
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	token, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	go p.persist(token) // Async persist — don't block caller
	return token, nil
}
