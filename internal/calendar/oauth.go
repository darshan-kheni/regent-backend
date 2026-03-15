package calendar

import (
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// oauth2Config is a package-local alias for oauth2.Config to keep google.go readable.
type oauth2Config = oauth2.Config

// stdOAuth2Token is a package-local alias for oauth2.Token.
type stdOAuth2Token = oauth2.Token

// googleEndpoint returns the Google OAuth2 endpoint.
func googleEndpoint() oauth2.Endpoint {
	return google.Endpoint
}

// oauth2HTTPClient returns an *http.Client that uses the given token source for auth.
func oauth2HTTPClient(src oauth2.TokenSource) *http.Client {
	return oauth2.NewClient(nil, src)
}
