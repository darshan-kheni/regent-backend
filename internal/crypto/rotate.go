package crypto

import (
	"fmt"

	"github.com/google/uuid"
)

// RotatingEncryptor wraps two Encryptors for zero-downtime key rotation.
// Decrypt: try current key first, fallback to previous key.
// Encrypt: always use current key.
type RotatingEncryptor struct {
	current  *Encryptor
	previous *Encryptor // nil if no rotation in progress
}

// NewRotatingEncryptor creates a RotatingEncryptor. previousKey may be empty if no rotation is in progress.
func NewRotatingEncryptor(currentKey string, previousKey string) (*RotatingEncryptor, error) {
	curr, err := NewEncryptor(currentKey)
	if err != nil {
		return nil, fmt.Errorf("current key: %w", err)
	}
	re := &RotatingEncryptor{current: curr}
	if previousKey != "" {
		prev, err := NewEncryptor(previousKey)
		if err != nil {
			return nil, fmt.Errorf("previous key: %w", err)
		}
		re.previous = prev
	}
	return re, nil
}

// EncryptForPurpose encrypts using the current key.
func (re *RotatingEncryptor) EncryptForPurpose(tenantID uuid.UUID, purpose string, plaintext []byte) ([]byte, []byte, error) {
	return re.current.EncryptForPurpose(tenantID, purpose, plaintext)
}

// DecryptForPurpose decrypts using the current key, falling back to the previous key.
func (re *RotatingEncryptor) DecryptForPurpose(tenantID uuid.UUID, purpose string, ciphertext, nonce []byte) ([]byte, error) {
	plaintext, err := re.current.DecryptForPurpose(tenantID, purpose, ciphertext, nonce)
	if err == nil {
		return plaintext, nil
	}
	if re.previous != nil {
		return re.previous.DecryptForPurpose(tenantID, purpose, ciphertext, nonce)
	}
	return nil, err
}

// Current returns the current encryptor for direct use.
func (re *RotatingEncryptor) Current() *Encryptor {
	return re.current
}
