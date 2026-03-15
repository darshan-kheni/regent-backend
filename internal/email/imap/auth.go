package imap

import (
	"fmt"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
)

// AuthenticatePlain authenticates with username/password (app passwords).
func AuthenticatePlain(client *imapclient.Client, username, password string) error {
	if err := client.Login(username, password).Wait(); err != nil {
		return fmt.Errorf("IMAP LOGIN: %w", err)
	}
	return nil
}

// AuthenticateXOAuth2 authenticates with OAuth2 bearer token using the
// XOAUTH2 mechanism (used by Gmail and Outlook).
//
// Required scope for Gmail: https://mail.google.com/
// Required scope for Outlook: https://outlook.office365.com/IMAP.AccessAsUser.All
func AuthenticateXOAuth2(client *imapclient.Client, email, accessToken string) error {
	saslClient := &xoauth2Client{
		username:    email,
		accessToken: accessToken,
	}
	if err := client.Authenticate(saslClient); err != nil {
		return fmt.Errorf("IMAP XOAUTH2: %w", err)
	}
	return nil
}

// AuthenticateOAuthBearer authenticates using the OAUTHBEARER mechanism
// (RFC 7628). Some providers prefer this over XOAUTH2.
func AuthenticateOAuthBearer(client *imapclient.Client, email, accessToken string) error {
	saslClient := sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
		Username: email,
		Token:    accessToken,
	})
	if err := client.Authenticate(saslClient); err != nil {
		return fmt.Errorf("IMAP OAUTHBEARER: %w", err)
	}
	return nil
}

// xoauth2Client implements the sasl.Client interface for the XOAUTH2 mechanism.
// Protocol spec: https://developers.google.com/gmail/imap/xoauth2-protocol
type xoauth2Client struct {
	username    string
	accessToken string
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	// XOAUTH2 initial response: "user=" + user + "\x01auth=Bearer " + token + "\x01\x01"
	resp := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", c.username, c.accessToken)
	return "XOAUTH2", []byte(resp), nil
}

func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	// XOAUTH2 sends an empty response to server challenges (error responses).
	// The server will then send a tagged NO response.
	return []byte{}, nil
}
