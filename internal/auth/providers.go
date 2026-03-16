package auth

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/database"
)

// OAuthTokens holds decrypted OAuth tokens for a provider.
type OAuthTokens struct {
	AccessToken   string
	RefreshToken  string
	Provider      string
	Scopes        []string
	ExpiresAt     time.Time
	ProviderEmail string
}

// OAuthTokenStore handles encrypted storage of OAuth provider tokens.
type OAuthTokenStore struct {
	pool      *pgxpool.Pool
	encryptor *crypto.Encryptor
}

// NewOAuthTokenStore creates a new token store.
func NewOAuthTokenStore(pool *pgxpool.Pool, enc *crypto.Encryptor) *OAuthTokenStore {
	return &OAuthTokenStore{pool: pool, encryptor: enc}
}

// StoreTokens encrypts and stores OAuth tokens. Uses UPSERT to handle re-auth.
// Each token is encrypted with an independent nonce (AES-GCM nonce reuse = broken security).
func (s *OAuthTokenStore) StoreTokens(ctx database.TenantContext, userID uuid.UUID, provider string, accessToken, refreshToken string, scopes []string, expiresAt time.Time, providerUserID, providerEmail string) error {
	accessCipher, accessNonce, err := s.encryptor.Encrypt(ctx.TenantID, []byte(accessToken))
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	refreshCipher, refreshNonce, err := s.encryptor.Encrypt(ctx.TenantID, []byte(refreshToken))
	if err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO oauth_provider_tokens
		 (user_id, tenant_id, provider, access_token_encrypted, access_token_nonce,
		  refresh_token_encrypted, refresh_token_nonce, scopes, expires_at,
		  provider_user_id, provider_email)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (user_id, provider) DO UPDATE SET
		   access_token_encrypted = EXCLUDED.access_token_encrypted,
		   access_token_nonce = EXCLUDED.access_token_nonce,
		   refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
		   refresh_token_nonce = EXCLUDED.refresh_token_nonce,
		   scopes = EXCLUDED.scopes,
		   expires_at = EXCLUDED.expires_at,
		   last_refreshed_at = now()`,
		userID, ctx.TenantID, provider,
		accessCipher, accessNonce, refreshCipher, refreshNonce,
		scopes, expiresAt, providerUserID, providerEmail,
	)
	if err != nil {
		return fmt.Errorf("store tokens: %w", err)
	}
	return nil
}

// GetTokens retrieves and decrypts OAuth tokens for a user+provider.
func (s *OAuthTokenStore) GetTokens(ctx database.TenantContext, userID uuid.UUID, provider string) (*OAuthTokens, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting tenant context: %w", err)
	}

	var accessCipher, accessNonce, refreshCipher, refreshNonce []byte
	var scopes []string
	var expiresAt *time.Time
	var providerEmail *string

	err = conn.QueryRow(ctx,
		`SELECT access_token_encrypted, access_token_nonce,
		        refresh_token_encrypted, refresh_token_nonce,
		        scopes, expires_at, provider_email
		 FROM oauth_provider_tokens
		 WHERE user_id = $1 AND provider = $2`,
		userID, provider,
	).Scan(&accessCipher, &accessNonce, &refreshCipher, &refreshNonce, &scopes, &expiresAt, &providerEmail)
	if err != nil {
		return nil, fmt.Errorf("get tokens: %w", err)
	}

	accessPlain, err := s.encryptor.Decrypt(ctx.TenantID, accessCipher, accessNonce)
	if err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}
	refreshPlain, err := s.encryptor.Decrypt(ctx.TenantID, refreshCipher, refreshNonce)
	if err != nil {
		return nil, fmt.Errorf("decrypt refresh token: %w", err)
	}

	tokens := &OAuthTokens{
		AccessToken:  string(accessPlain),
		RefreshToken: string(refreshPlain),
		Provider:     provider,
		Scopes:       scopes,
	}
	if expiresAt != nil {
		tokens.ExpiresAt = *expiresAt
	}
	if providerEmail != nil {
		tokens.ProviderEmail = *providerEmail
	}
	return tokens, nil
}

// DeleteTokens removes OAuth tokens for a user+provider.
func (s *OAuthTokenStore) DeleteTokens(ctx database.TenantContext, userID uuid.UUID, provider string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`DELETE FROM oauth_provider_tokens WHERE user_id = $1 AND provider = $2`,
		userID, provider,
	)
	return err
}

// EnsureEmailAccount creates a user_accounts entry for an OAuth-connected email if it doesn't exist.
func (s *OAuthTokenStore) EnsureEmailAccount(ctx database.TenantContext, userID uuid.UUID, provider, email string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO user_accounts (user_id, tenant_id, provider, email_address, display_name, sync_status)
		 VALUES ($1, $2, $3, $4, $5, 'active')
		 ON CONFLICT (user_id, email_address) DO UPDATE SET
		   provider = EXCLUDED.provider,
		   sync_status = 'active'`,
		userID, ctx.TenantID, provider, email, email,
	)
	if err != nil {
		return fmt.Errorf("ensuring email account: %w", err)
	}
	return nil
}
