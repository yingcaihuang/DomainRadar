package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// MasterKeyEnvVar is the environment variable name for the master encryption key.
	MasterKeyEnvVar = "DOMAINRADAR_MASTER_KEY"

	// keyLength is the required length for AES-256 keys (32 bytes).
	keyLength = 32

	// nonceSize is the standard nonce size for AES-GCM (12 bytes).
	nonceSize = 12
)

// CryptoService provides AES-256-GCM encryption and credential masking.
type CryptoService struct {
	gcm cipher.AEAD
}

// NewCryptoService creates a new CryptoService with the given master key.
// The key must be exactly 32 bytes (256 bits) for AES-256.
func NewCryptoService(masterKey string) (*CryptoService, error) {
	if len(masterKey) != keyLength {
		return nil, fmt.Errorf("master key must be exactly %d bytes, got %d", keyLength, len(masterKey))
	}

	block, err := aes.NewCipher([]byte(masterKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &CryptoService{gcm: gcm}, nil
}

// NewCryptoServiceFromEnv creates a CryptoService using the master key from
// the DOMAINRADAR_MASTER_KEY environment variable.
func NewCryptoServiceFromEnv() (*CryptoService, error) {
	key := os.Getenv(MasterKeyEnvVar)
	if key == "" {
		return nil, fmt.Errorf("environment variable %s is not set", MasterKeyEnvVar)
	}
	return NewCryptoService(key)
}

// Encrypt encrypts plaintext using AES-256-GCM with a unique random nonce.
// The result is base64-encoded with the nonce prepended to the ciphertext.
func (s *CryptoService) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext that was encrypted with Encrypt.
// It extracts the nonce from the first 12 bytes and decrypts the remainder.
func (s *CryptoService) Decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to base64 decode: %w", err)
	}

	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := s.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// MaskCredential masks a credential string, showing only the last 4 characters.
// For strings with 4 or fewer characters, it returns "****".
func MaskCredential(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}
