package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SupabaseClient wraps the Supabase Admin API for server-side auth operations.
type SupabaseClient struct {
	httpClient *http.Client
	baseURL    string
	serviceKey string
}

// SupabaseUser represents a user from the Supabase Admin API.
type SupabaseUser struct {
	ID          uuid.UUID              `json:"id"`
	Email       string                 `json:"email"`
	AppMetadata map[string]interface{} `json:"app_metadata"`
	Identities  []SupabaseIdentity     `json:"identities"`
}

// SupabaseIdentity represents an OAuth identity linked to a user.
type SupabaseIdentity struct {
	Provider     string                 `json:"provider"`
	IdentityData map[string]interface{} `json:"identity_data"`
}

// NewSupabaseClient creates a new Supabase Admin API client.
func NewSupabaseClient(cfg Config) *SupabaseClient {
	return &SupabaseClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    cfg.SupabaseURL,
		serviceKey: cfg.SupabaseServiceKey,
	}
}

// GetUser retrieves a user by ID via the Supabase Admin API.
func (c *SupabaseClient) GetUser(ctx context.Context, userID uuid.UUID) (*SupabaseUser, error) {
	url := fmt.Sprintf("%s/auth/v1/admin/users/%s", c.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("apikey", c.serviceKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get user failed (%d): %s", resp.StatusCode, body)
	}

	var user SupabaseUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &user, nil
}

// UpdateUserMetadata updates a user's app_metadata via the Supabase Admin API.
func (c *SupabaseClient) UpdateUserMetadata(ctx context.Context, userID uuid.UUID, appMeta map[string]interface{}) error {
	url := fmt.Sprintf("%s/auth/v1/admin/users/%s", c.baseURL, userID)
	body, _ := json.Marshal(map[string]interface{}{"app_metadata": appMeta})

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update metadata failed (%d): %s", resp.StatusCode, respBody)
	}
	return nil
}

// SignOutUser signs out a user via the Supabase Admin API.
// scope can be "global" (all sessions), "local" (current), or "others" (all except current).
func (c *SupabaseClient) SignOutUser(ctx context.Context, userID uuid.UUID, scope string) error {
	url := fmt.Sprintf("%s/auth/v1/admin/users/%s/logout", c.baseURL, userID)
	body, _ := json.Marshal(map[string]string{"scope": scope})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sign out user: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// SignUpResponse holds the response from Supabase signup.
type SignUpResponse struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
}

// LoginResponse holds the response from Supabase login.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	User         struct {
		ID    uuid.UUID `json:"id"`
		Email string    `json:"email"`
	} `json:"user"`
}

// SignUp creates a new user via Supabase Auth.
func (c *SupabaseClient) SignUp(ctx context.Context, email, password string) (*SignUpResponse, error) {
	url := fmt.Sprintf("%s/auth/v1/signup", c.baseURL)
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("signup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("signup failed (%d): %s", resp.StatusCode, respBody)
	}

	var result SignUpResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode signup response: %w", err)
	}
	return &result, nil
}

// Login authenticates a user via Supabase Auth.
func (c *SupabaseClient) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	url := fmt.Sprintf("%s/auth/v1/token?grant_type=password", c.baseURL)
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login failed (%d): %s", resp.StatusCode, respBody)
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}
	return &result, nil
}

// RefreshToken refreshes an access token via Supabase Auth.
func (c *SupabaseClient) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	url := fmt.Sprintf("%s/auth/v1/token?grant_type=refresh_token", c.baseURL)
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, respBody)
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	return &result, nil
}

// UpdateUserPassword updates a user's password via the Supabase Admin API.
func (c *SupabaseClient) UpdateUserPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	url := fmt.Sprintf("%s/auth/v1/admin/users/%s", c.baseURL, userID)
	body, _ := json.Marshal(map[string]string{"password": newPassword})

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update password failed (%d): %s", resp.StatusCode, respBody)
	}
	return nil
}

// ResetPasswordForEmail sends a password reset email via the Supabase Auth API.
func (c *SupabaseClient) ResetPasswordForEmail(ctx context.Context, email string) error {
	url := fmt.Sprintf("%s/auth/v1/recover", c.baseURL)
	body, _ := json.Marshal(map[string]string{"email": email})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("apikey", c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	defer resp.Body.Close()
	return nil
}
