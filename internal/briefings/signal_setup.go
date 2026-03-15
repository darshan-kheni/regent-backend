package briefings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SignalSetup handles Signal number registration and verification.
type SignalSetup struct {
	apiURL     string
	httpClient *http.Client
}

// NewSignalSetup creates a setup helper for Signal registration.
func NewSignalSetup(apiURL string) *SignalSetup {
	return &SignalSetup{
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// HealthCheck verifies that the signal-cli-rest-api container is running.
func (s *SignalSetup) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/about", s.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("signal setup: create health request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("signal setup: API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal setup: health check failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// Register initiates phone number registration with Signal.
// Requires a captcha token obtained from the Signal captcha page.
func (s *SignalSetup) Register(ctx context.Context, phoneNumber, captcha string) error {
	payload := map[string]string{"captcha": captcha}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/v1/register/%s", s.apiURL, phoneNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("signal setup: create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("signal setup: register request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal setup: registration failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Verify completes registration by providing the SMS verification code.
func (s *SignalSetup) Verify(ctx context.Context, phoneNumber, code string) error {
	payload := map[string]string{"token": code}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/v1/register/%s/verify", s.apiURL, phoneNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("signal setup: create verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("signal setup: verify request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal setup: verification failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
