package email

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/database"
)

const (
	// PurposeIMAPCredentials is the HKDF purpose string for IMAP credential encryption.
	PurposeIMAPCredentials = "regent-imap-credentials"
	// PurposeSMTPCredentials is the HKDF purpose string for SMTP credential encryption.
	PurposeSMTPCredentials = "regent-smtp-credentials"
)

// CredentialStore manages encrypted IMAP/SMTP credentials.
type CredentialStore struct {
	encryptor *crypto.RotatingEncryptor
	pool      *pgxpool.Pool
}

// NewCredentialStore creates a new CredentialStore.
func NewCredentialStore(encryptor *crypto.RotatingEncryptor, pool *pgxpool.Pool) *CredentialStore {
	return &CredentialStore{encryptor: encryptor, pool: pool}
}

func purposeForType(credType string) string {
	if credType == "smtp_password" {
		return PurposeSMTPCredentials
	}
	return PurposeIMAPCredentials
}

// StoreCredential encrypts and stores a credential in the database.
func (cs *CredentialStore) StoreCredential(ctx database.TenantContext, accountID uuid.UUID, credType, plaintext string) error {
	purpose := purposeForType(credType)

	ciphertext, nonce, err := cs.encryptor.EncryptForPurpose(ctx.TenantID, purpose, []byte(plaintext))
	if err != nil {
		return fmt.Errorf("encrypting credential: %w", err)
	}

	conn, err := cs.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()
	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting tenant context: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO email_credentials (tenant_id, account_id, credential_type, encrypted_value, encryption_nonce, encryption_key_version)
		 VALUES ($1, $2, $3, $4, $5, 1)
		 ON CONFLICT (account_id, credential_type) DO UPDATE SET
		   encrypted_value = EXCLUDED.encrypted_value,
		   encryption_nonce = EXCLUDED.encryption_nonce,
		   encryption_key_version = EXCLUDED.encryption_key_version,
		   updated_at = now()`,
		ctx.TenantID, accountID, credType, ciphertext, nonce)
	if err != nil {
		return fmt.Errorf("storing credential: %w", err)
	}
	return nil
}

// Encryptor returns the underlying encryptor for direct use in background operations.
func (cs *CredentialStore) Encryptor() *crypto.RotatingEncryptor {
	return cs.encryptor
}

// GetCredential retrieves and decrypts a credential from the database.
func (cs *CredentialStore) GetCredential(ctx database.TenantContext, accountID uuid.UUID, credType string) (string, error) {
	purpose := purposeForType(credType)

	conn, err := cs.pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()
	if err := database.SetRLSContext(ctx, conn); err != nil {
		return "", fmt.Errorf("setting tenant context: %w", err)
	}

	var ciphertext, nonce []byte
	err = conn.QueryRow(ctx,
		`SELECT encrypted_value, encryption_nonce FROM email_credentials
		 WHERE account_id = $1 AND credential_type = $2`,
		accountID, credType).Scan(&ciphertext, &nonce)
	if err != nil {
		return "", fmt.Errorf("fetching credential: %w", err)
	}

	plainBytes, err := cs.encryptor.DecryptForPurpose(ctx.TenantID, purpose, ciphertext, nonce)
	if err != nil {
		return "", fmt.Errorf("decrypting credential: %w", err)
	}
	return string(plainBytes), nil
}
