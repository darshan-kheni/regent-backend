package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/darshan-kheni/regent/internal/config"
)

// OAuthPopupHandlers handles the popup-based OAuth flow for connecting
// additional email accounts (Google/Microsoft) without replacing the
// user's existing Supabase session.
type OAuthPopupHandlers struct {
	googleClientID       string
	googleClientSecret   string
	microsoftClientID    string
	microsoftClientSecret string
	redirectURI          string // e.g. http://localhost:8080/api/v1/oauth/callback
	frontendOrigin       string // e.g. http://localhost:3000 — for postMessage targetOrigin
}

// NewOAuthPopupHandlers creates a new OAuthPopupHandlers instance from config.
func NewOAuthPopupHandlers(cfg *config.Config) *OAuthPopupHandlers {
	backendPort := cfg.Port
	if backendPort == "" {
		backendPort = "8080"
	}

	return &OAuthPopupHandlers{
		googleClientID:        cfg.Gmail.ClientID,
		googleClientSecret:    cfg.Gmail.ClientSecret,
		microsoftClientID:     cfg.Microsoft.ClientID,
		microsoftClientSecret: cfg.Microsoft.ClientSecret,
		redirectURI:           fmt.Sprintf("http://localhost:%s/api/v1/oauth/callback", backendPort),
		frontendOrigin:        cfg.Billing.FrontendURL,
	}
}

// HandleOAuthStart handles GET /api/v1/oauth/start?provider=google|microsoft.
// It redirects the popup window to the provider's OAuth consent screen.
func (h *OAuthPopupHandlers) HandleOAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider != "google" && provider != "microsoft" {
		http.Error(w, "invalid provider: must be 'google' or 'microsoft'", http.StatusBadRequest)
		return
	}

	state, err := generateState()
	if err != nil {
		slog.Error("failed to generate OAuth state", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Encode provider into state so we know which provider on callback
	stateValue := provider + ":" + state

	var authURL string
	switch provider {
	case "google":
		authURL = h.buildGoogleAuthURL(stateValue)
	case "microsoft":
		authURL = h.buildMicrosoftAuthURL(stateValue)
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleOAuthCallback handles GET /api/v1/oauth/callback?code=...&state=...
// It exchanges the authorization code for tokens and returns an HTML page
// that sends the tokens to the parent window via postMessage, then closes.
func (h *OAuthPopupHandlers) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	oauthError := r.URL.Query().Get("error")

	// Handle provider-side errors (user denied, etc.)
	if oauthError != "" {
		errorDesc := r.URL.Query().Get("error_description")
		if errorDesc == "" {
			errorDesc = oauthError
		}
		h.renderCallbackPage(w, "", nil, errorDesc)
		return
	}

	if code == "" || state == "" {
		h.renderCallbackPage(w, "", nil, "missing code or state parameter")
		return
	}

	// Extract provider from state (format: "provider:randomhex")
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		h.renderCallbackPage(w, "", nil, "invalid state parameter")
		return
	}
	provider := parts[0]

	if provider != "google" && provider != "microsoft" {
		h.renderCallbackPage(w, "", nil, "invalid provider in state")
		return
	}

	// Exchange authorization code for tokens
	tokenResp, err := h.exchangeCode(provider, code)
	if err != nil {
		slog.Error("OAuth token exchange failed", "provider", provider, "error", err)
		h.renderCallbackPage(w, provider, nil, "failed to exchange authorization code")
		return
	}

	// Extract email from ID token or userinfo
	email, err := h.fetchUserEmail(provider, tokenResp.AccessToken)
	if err != nil {
		slog.Warn("failed to fetch user email from OAuth provider", "provider", provider, "error", err)
		// Non-fatal: we can proceed without the email
	}

	h.renderCallbackPage(w, provider, &callbackTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		Email:        email,
	}, "")
}

// callbackTokens holds the tokens to send to the parent window.
type callbackTokens struct {
	AccessToken  string
	RefreshToken string
	Email        string
}

// tokenExchangeResponse holds the response from the token endpoint.
type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
}

func (h *OAuthPopupHandlers) buildGoogleAuthURL(state string) string {
	params := url.Values{
		"client_id":     {h.googleClientID},
		"redirect_uri":  {h.redirectURI},
		"response_type": {"code"},
		"scope":         {"openid email profile https://mail.google.com/"},
		"state":         {state},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func (h *OAuthPopupHandlers) buildMicrosoftAuthURL(state string) string {
	params := url.Values{
		"client_id":     {h.microsoftClientID},
		"redirect_uri":  {h.redirectURI},
		"response_type": {"code"},
		"scope":         {"openid email profile offline_access Mail.Read Mail.Send IMAP.AccessAsUser.All"},
		"state":         {state},
		"prompt":        {"consent"},
	}
	return "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?" + params.Encode()
}

func (h *OAuthPopupHandlers) exchangeCode(provider, code string) (*tokenExchangeResponse, error) {
	var tokenURL string
	var clientID, clientSecret string

	switch provider {
	case "google":
		tokenURL = "https://oauth2.googleapis.com/token"
		clientID = h.googleClientID
		clientSecret = h.googleClientSecret
	case "microsoft":
		tokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
		clientID = h.microsoftClientID
		clientSecret = h.microsoftClientSecret
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {h.redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenExchangeResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	return &tokenResp, nil
}

// fetchUserEmail retrieves the user's email from the provider's userinfo endpoint.
func (h *OAuthPopupHandlers) fetchUserEmail(provider, accessToken string) (string, error) {
	var userinfoURL string
	switch provider {
	case "google":
		userinfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
	case "microsoft":
		userinfoURL = "https://graph.microsoft.com/v1.0/me"
	default:
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}

	req, err := http.NewRequest("GET", userinfoURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Email string `json:"email"`
		Mail  string `json:"mail"`                // Microsoft Graph uses "mail"
		UPN   string `json:"userPrincipalName"`   // Microsoft fallback
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing userinfo response: %w", err)
	}

	email := result.Email
	if email == "" {
		email = result.Mail
	}
	if email == "" {
		email = result.UPN
	}

	return email, nil
}

// renderCallbackPage writes an HTML page that sends the OAuth result to the
// parent window via postMessage and then closes itself.
func (h *OAuthPopupHandlers) renderCallbackPage(w http.ResponseWriter, provider string, tokens *callbackTokens, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	data := struct {
		Provider     string
		AccessToken  string
		RefreshToken string
		Email        string
		Error        string
		Origin       string
	}{
		Provider: provider,
		Origin:   h.frontendOrigin,
	}

	if tokens != nil {
		data.AccessToken = tokens.AccessToken
		data.RefreshToken = tokens.RefreshToken
		data.Email = tokens.Email
	}
	if errMsg != "" {
		data.Error = errMsg
	}

	if err := callbackTemplate.Execute(w, data); err != nil {
		slog.Error("failed to render OAuth callback page", "error", err)
	}
}

// generateState creates a cryptographically random state string.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// callbackTemplate is the HTML page returned to the OAuth popup after token exchange.
// It sends the result to the parent window via postMessage and closes itself.
var callbackTemplate = template.Must(template.New("oauth-callback").Parse(`<!DOCTYPE html>
<html>
<head><title>Regent - Connecting Account</title></head>
<body>
<p>Connecting your account... This window will close automatically.</p>
<script>
(function() {
  var msg = {
    type: 'oauth-callback',
    provider: '{{.Provider}}',
    access_token: '{{.AccessToken}}',
    refresh_token: '{{.RefreshToken}}',
    email: '{{.Email}}',
    error: '{{.Error}}'
  };
  if (window.opener) {
    window.opener.postMessage(msg, '{{.Origin}}');
  }
  window.close();
})();
</script>
</body>
</html>`))
