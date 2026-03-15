package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"
)

// Encryptor provides AES-256-GCM encryption with per-tenant key derivation.
type Encryptor struct {
	masterKey []byte
}

// NewEncryptor creates an Encryptor from a base64-encoded 32-byte master key.
func NewEncryptor(masterKeyBase64 string) (*Encryptor, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	return &Encryptor{masterKey: key}, nil
}

// DeriveKey derives a per-tenant 32-byte key using HKDF-SHA256.
func (e *Encryptor) DeriveKey(tenantID uuid.UUID) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, e.masterKey, []byte(tenantID.String()), []byte("regent-oauth-tokens"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}

// DeriveKeyForPurpose derives a 32-byte key using HKDF-SHA256 with a specific purpose string.
// Different purposes produce different keys from the same master key + tenant ID.
func (e *Encryptor) DeriveKeyForPurpose(tenantID uuid.UUID, purpose string) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, e.masterKey, []byte(tenantID.String()), []byte(purpose))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, key); err != nil {
		return nil, fmt.Errorf("derive key for %s: %w", purpose, err)
	}
	return key, nil
}

// EncryptForPurpose encrypts plaintext with a purpose-specific derived key.
func (e *Encryptor) EncryptForPurpose(tenantID uuid.UUID, purpose string, plaintext []byte) (ciphertext, nonce []byte, err error) {
	key, err := e.DeriveKeyForPurpose(tenantID, purpose)
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("create cipher: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create GCM: %w", err)
	}
	nonce = make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext = aesGCM.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// DecryptForPurpose decrypts ciphertext with a purpose-specific derived key.
func (e *Encryptor) DecryptForPurpose(tenantID uuid.UUID, purpose string, ciphertext, nonce []byte) ([]byte, error) {
	key, err := e.DeriveKeyForPurpose(tenantID, purpose)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
