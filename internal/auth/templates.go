package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// EmailTemplateConfig holds Supabase Auth email template configuration.
type EmailTemplateConfig struct {
	ConfirmationSubject string
	ConfirmationBody    string
	RecoverySubject     string
	RecoveryBody        string
	MagicLinkSubject    string
	MagicLinkBody       string
}

// DefaultEmailTemplates returns the branded Regent email templates.
func DefaultEmailTemplates() EmailTemplateConfig {
	return EmailTemplateConfig{
		ConfirmationSubject: "Confirm your Regent account",
		ConfirmationBody:    confirmationTemplate,
		RecoverySubject:     "Reset your Regent password",
		RecoveryBody:        recoveryTemplate,
		MagicLinkSubject:    "Sign in to Regent",
		MagicLinkBody:       magicLinkTemplate,
	}
}

// UpdateEmailTemplates applies branded templates via the Supabase Management API.
// projectRef is the Supabase project reference (e.g., "abcdefghijklmnop").
func (c *SupabaseClient) UpdateEmailTemplates(ctx context.Context, projectRef string, templates EmailTemplateConfig) error {
	url := fmt.Sprintf("https://api.supabase.com/v1/projects/%s/config/auth", projectRef)

	payload := map[string]string{
		"MAILER_TEMPLATES_CONFIRMATION": templates.ConfirmationBody,
		"MAILER_TEMPLATES_RECOVERY":     templates.RecoveryBody,
		"MAILER_TEMPLATES_MAGIC_LINK":   templates.MagicLinkBody,
		"MAILER_SUBJECTS_CONFIRMATION":  templates.ConfirmationSubject,
		"MAILER_SUBJECTS_RECOVERY":      templates.RecoverySubject,
		"MAILER_SUBJECTS_MAGIC_LINK":    templates.MagicLinkSubject,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal templates: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update templates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update templates failed (%d): %s", resp.StatusCode, respBody)
	}
	return nil
}

// Template constants — Regent branded with EB Garamond headers, Inter body, gold #C9A96E accents, zero border-radius
const confirmationTemplate = `<!DOCTYPE html>
<html>
<head>
  <link href="https://fonts.googleapis.com/css2?family=EB+Garamond:wght@500;700&family=Inter:wght@400;500&display=swap" rel="stylesheet">
  <style>
    body { margin:0; padding:0; background:#1A1A1A; font-family:Inter,sans-serif; color:#E5E5E5; }
    .container { max-width:560px; margin:40px auto; background:#242424; border:1px solid #333; }
    .header { padding:32px; text-align:center; border-bottom:2px solid #C9A96E; }
    .header h1 { font-family:"EB Garamond",serif; color:#C9A96E; font-size:28px; margin:0; letter-spacing:1px; }
    .body { padding:32px; line-height:1.6; }
    .btn { display:inline-block; padding:14px 32px; background:#C9A96E; color:#1A1A1A; text-decoration:none; font-weight:600; font-family:Inter,sans-serif; }
    .footer { padding:24px 32px; text-align:center; font-size:12px; color:#888; border-top:1px solid #333; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header"><h1>REGENT</h1></div>
    <div class="body">
      <p>Welcome to Regent. Please confirm your email address to activate your AI executive assistant.</p>
      <p style="text-align:center;margin:32px 0;"><a href="{{ .ConfirmationURL }}" class="btn">Confirm Email</a></p>
      <p style="font-size:13px;color:#888;">If you did not create an account, you can safely ignore this email.</p>
    </div>
    <div class="footer">Regent &mdash; Your AI Executive Assistant</div>
  </div>
</body>
</html>`

const recoveryTemplate = `<!DOCTYPE html>
<html>
<head>
  <link href="https://fonts.googleapis.com/css2?family=EB+Garamond:wght@500;700&family=Inter:wght@400;500&display=swap" rel="stylesheet">
  <style>
    body { margin:0; padding:0; background:#1A1A1A; font-family:Inter,sans-serif; color:#E5E5E5; }
    .container { max-width:560px; margin:40px auto; background:#242424; border:1px solid #333; }
    .header { padding:32px; text-align:center; border-bottom:2px solid #C9A96E; }
    .header h1 { font-family:"EB Garamond",serif; color:#C9A96E; font-size:28px; margin:0; letter-spacing:1px; }
    .body { padding:32px; line-height:1.6; }
    .btn { display:inline-block; padding:14px 32px; background:#C9A96E; color:#1A1A1A; text-decoration:none; font-weight:600; font-family:Inter,sans-serif; }
    .footer { padding:24px 32px; text-align:center; font-size:12px; color:#888; border-top:1px solid #333; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header"><h1>REGENT</h1></div>
    <div class="body">
      <p>We received a request to reset your Regent password.</p>
      <p style="text-align:center;margin:32px 0;"><a href="{{ .ConfirmationURL }}" class="btn">Reset Password</a></p>
      <p style="font-size:13px;color:#888;">This link expires in 1 hour. If you did not request a password reset, you can safely ignore this email.</p>
    </div>
    <div class="footer">Regent &mdash; Your AI Executive Assistant</div>
  </div>
</body>
</html>`

const magicLinkTemplate = `<!DOCTYPE html>
<html>
<head>
  <link href="https://fonts.googleapis.com/css2?family=EB+Garamond:wght@500;700&family=Inter:wght@400;500&display=swap" rel="stylesheet">
  <style>
    body { margin:0; padding:0; background:#1A1A1A; font-family:Inter,sans-serif; color:#E5E5E5; }
    .container { max-width:560px; margin:40px auto; background:#242424; border:1px solid #333; }
    .header { padding:32px; text-align:center; border-bottom:2px solid #C9A96E; }
    .header h1 { font-family:"EB Garamond",serif; color:#C9A96E; font-size:28px; margin:0; letter-spacing:1px; }
    .body { padding:32px; line-height:1.6; }
    .btn { display:inline-block; padding:14px 32px; background:#C9A96E; color:#1A1A1A; text-decoration:none; font-weight:600; font-family:Inter,sans-serif; }
    .footer { padding:24px 32px; text-align:center; font-size:12px; color:#888; border-top:1px solid #333; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header"><h1>REGENT</h1></div>
    <div class="body">
      <p>Click the link below to sign in to your Regent account.</p>
      <p style="text-align:center;margin:32px 0;"><a href="{{ .ConfirmationURL }}" class="btn">Sign In</a></p>
      <p style="font-size:13px;color:#888;">This link expires in 5 minutes. If you did not request this, you can safely ignore this email.</p>
    </div>
    <div class="footer">Regent &mdash; Your AI Executive Assistant</div>
  </div>
</body>
</html>`
