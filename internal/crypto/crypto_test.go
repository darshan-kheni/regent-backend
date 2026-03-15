package crypto

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMasterKey() string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestNewEncryptor_Valid(t *testing.T) {
	enc, err := NewEncryptor(testMasterKey())
	require.NoError(t, err)
	assert.NotNil(t, enc)
}

func TestNewEncryptor_InvalidBase64(t *testing.T) {
	_, err := NewEncryptor("not-valid-base64!!!")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decode master key")
}

func TestNewEncryptor_WrongLength(t *testing.T) {
	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := NewEncryptor(shortKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestDeriveKey_Deterministic(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	key1, err := enc.DeriveKey(tenantID)
	require.NoError(t, err)
	key2, err := enc.DeriveKey(tenantID)
	require.NoError(t, err)

	assert.Equal(t, key1, key2, "same tenant should derive same key")
}

func TestDeriveKey_DifferentTenants(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	keyA, _ := enc.DeriveKey(tenantA)
	keyB, _ := enc.DeriveKey(tenantB)

	assert.NotEqual(t, keyA, keyB, "different tenants must derive different keys")
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.New()
	plaintext := []byte("super-secret-oauth-token-12345")

	ciphertext, nonce, err := enc.Encrypt(tenantID, plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := enc.Decrypt(tenantID, ciphertext, nonce)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestDecrypt_WrongTenant(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantA := uuid.New()
	tenantB := uuid.New()

	ciphertext, nonce, _ := enc.Encrypt(tenantA, []byte("secret"))

	_, err := enc.Decrypt(tenantB, ciphertext, nonce)
	assert.Error(t, err, "decrypting with wrong tenant key should fail")
}

func TestEncrypt_UniqueNonces(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.New()

	_, nonce1, _ := enc.Encrypt(tenantID, []byte("data1"))
	_, nonce2, _ := enc.Encrypt(tenantID, []byte("data2"))

	assert.NotEqual(t, nonce1, nonce2, "each encryption must use a unique nonce")
}

func TestDeriveKeyForPurpose_DomainSeparation(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	keyA, err := enc.DeriveKeyForPurpose(tenantID, "regent-imap-credentials")
	require.NoError(t, err)
	keyB, err := enc.DeriveKeyForPurpose(tenantID, "regent-smtp-credentials")
	require.NoError(t, err)
	keyC, err := enc.DeriveKeyForPurpose(tenantID, "regent-oauth-tokens")
	require.NoError(t, err)

	assert.NotEqual(t, keyA, keyB, "different purposes must produce different keys")
	assert.NotEqual(t, keyA, keyC)
	assert.NotEqual(t, keyB, keyC)
}

func TestDeriveKeyForPurpose_Deterministic(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.New()

	key1, _ := enc.DeriveKeyForPurpose(tenantID, "regent-imap-credentials")
	key2, _ := enc.DeriveKeyForPurpose(tenantID, "regent-imap-credentials")
	assert.Equal(t, key1, key2)
}

func TestEncryptDecryptForPurpose_RoundTrip(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.New()
	plaintext := []byte("my-secret-imap-password")

	ciphertext, nonce, err := enc.EncryptForPurpose(tenantID, "regent-imap-credentials", plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := enc.DecryptForPurpose(tenantID, "regent-imap-credentials", ciphertext, nonce)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestDecryptForPurpose_WrongPurpose(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.New()

	ciphertext, nonce, _ := enc.EncryptForPurpose(tenantID, "regent-imap-credentials", []byte("secret"))

	_, err := enc.DecryptForPurpose(tenantID, "regent-smtp-credentials", ciphertext, nonce)
	assert.Error(t, err, "decrypting with wrong purpose should fail")
}

func TestRotatingEncryptor_EncryptDecrypt(t *testing.T) {
	re, err := NewRotatingEncryptor(testMasterKey(), "")
	require.NoError(t, err)

	tenantID := uuid.New()
	plaintext := []byte("test-password")

	ciphertext, nonce, err := re.EncryptForPurpose(tenantID, "regent-imap-credentials", plaintext)
	require.NoError(t, err)

	decrypted, err := re.DecryptForPurpose(tenantID, "regent-imap-credentials", ciphertext, nonce)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestRotatingEncryptor_DualRead(t *testing.T) {
	oldKey := testMasterKey()
	// Create a different key for "new"
	newKeyBytes := make([]byte, 32)
	for i := range newKeyBytes {
		newKeyBytes[i] = byte(i + 100)
	}
	newKey := base64.StdEncoding.EncodeToString(newKeyBytes)

	// Encrypt with old key
	oldEnc, _ := NewEncryptor(oldKey)
	tenantID := uuid.New()
	ciphertext, nonce, _ := oldEnc.EncryptForPurpose(tenantID, "regent-imap-credentials", []byte("old-secret"))

	// Create RotatingEncryptor with new as current, old as previous
	re, err := NewRotatingEncryptor(newKey, oldKey)
	require.NoError(t, err)

	// Should decrypt via fallback to previous key
	decrypted, err := re.DecryptForPurpose(tenantID, "regent-imap-credentials", ciphertext, nonce)
	require.NoError(t, err)
	assert.Equal(t, []byte("old-secret"), decrypted)
}

func TestRotatingEncryptor_NoPrevious(t *testing.T) {
	re, err := NewRotatingEncryptor(testMasterKey(), "")
	require.NoError(t, err)

	// Encrypt with a different encryptor
	otherKeyBytes := make([]byte, 32)
	for i := range otherKeyBytes {
		otherKeyBytes[i] = byte(i + 50)
	}
	otherEnc, _ := NewEncryptor(base64.StdEncoding.EncodeToString(otherKeyBytes))
	tenantID := uuid.New()
	ciphertext, nonce, _ := otherEnc.EncryptForPurpose(tenantID, "regent-imap-credentials", []byte("data"))

	// Should fail — no previous key to fall back to
	_, err = re.DecryptForPurpose(tenantID, "regent-imap-credentials", ciphertext, nonce)
	assert.Error(t, err)
}

func TestDeriveKeyForPurpose_BackwardCompatible(t *testing.T) {
	enc, _ := NewEncryptor(testMasterKey())
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	// DeriveKey uses "regent-oauth-tokens" as info
	oldKey, err := enc.DeriveKey(tenantID)
	require.NoError(t, err)

	// DeriveKeyForPurpose with same purpose should produce same result
	newKey, err := enc.DeriveKeyForPurpose(tenantID, "regent-oauth-tokens")
	require.NoError(t, err)

	assert.Equal(t, oldKey, newKey, "DeriveKeyForPurpose('regent-oauth-tokens') must be backward compatible with DeriveKey")
}
