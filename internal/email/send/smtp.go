package send

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
)

// xoauth2Auth implements smtp.Auth for XOAUTH2 authentication.
type xoauth2Auth struct {
	email string
	token string
}

// NewXOAuth2Auth creates an smtp.Auth that implements the XOAUTH2 mechanism.
func NewXOAuth2Auth(email, token string) smtp.Auth {
	return &xoauth2Auth{email: email, token: token}
}

func (a *xoauth2Auth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	blob := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", a.email, a.token)
	return "XOAUTH2", []byte(blob), nil
}

func (a *xoauth2Auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, fmt.Errorf("xoauth2 rejected: %s", fromServer)
	}
	return nil, nil
}

// SendViaSMTP sends an email via SMTP with STARTTLS.
func SendViaSMTP(host string, port int, auth smtp.Auth, from string, to []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", addr, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	return c.Quit()
}
