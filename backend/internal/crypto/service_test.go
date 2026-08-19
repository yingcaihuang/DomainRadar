package crypto

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "01234567890123456789012345678901" // exactly 32 bytes

func TestNewCryptoService_ValidKey(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewCryptoService_InvalidKeyLength(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"too short", "short"},
		{"too long", testKey + "extra"},
		{"empty", ""},
		{"31 bytes", testKey[:31]},
		{"33 bytes", testKey + "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewCryptoService(tt.key)
			assert.Nil(t, svc)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "master key must be exactly 32 bytes")
		})
	}
}

func TestNewCryptoServiceFromEnv_Set(t *testing.T) {
	os.Setenv(MasterKeyEnvVar, testKey)
	defer os.Unsetenv(MasterKeyEnvVar)

	svc, err := NewCryptoServiceFromEnv()
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewCryptoServiceFromEnv_NotSet(t *testing.T) {
	os.Unsetenv(MasterKeyEnvVar)

	svc, err := NewCryptoServiceFromEnv()
	assert.Nil(t, svc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not set")
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)

	tests := []string{
		"hello world",
		"",
		"a",
		"super-secret-api-key-12345",
		"unicode: 你好世界 🌍",
		strings.Repeat("x", 1000),
	}

	for _, plaintext := range tests {
		t.Run(plaintext[:min(len(plaintext), 20)], func(t *testing.T) {
			encrypted, err := svc.Encrypt(plaintext)
			require.NoError(t, err)
			assert.NotEmpty(t, encrypted)

			decrypted, err := svc.Decrypt(encrypted)
			require.NoError(t, err)
			assert.Equal(t, plaintext, decrypted)
		})
	}
}

func TestEncrypt_UniqueNonce(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)

	// Encrypting the same plaintext twice should produce different ciphertexts
	enc1, err := svc.Encrypt("same-plaintext")
	require.NoError(t, err)

	enc2, err := svc.Encrypt("same-plaintext")
	require.NoError(t, err)

	assert.NotEqual(t, enc1, enc2, "encrypting same plaintext should produce different ciphertexts due to unique nonces")
}

func TestEncrypt_CiphertextDoesNotContainPlaintext(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)

	plaintext := "my-secret-api-key"
	encrypted, err := svc.Encrypt(plaintext)
	require.NoError(t, err)

	// The base64-encoded ciphertext should not contain the plaintext
	assert.NotContains(t, encrypted, plaintext)

	// Decode and check raw bytes too
	decoded, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
	assert.NotContains(t, string(decoded), plaintext)
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)

	_, err = svc.Decrypt("not-valid-base64!!!")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base64")
}

func TestDecrypt_TooShort(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)

	// Encode just a few bytes (less than nonce size)
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err = svc.Decrypt(short)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext too short")
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	svc, err := NewCryptoService(testKey)
	require.NoError(t, err)

	encrypted, err := svc.Encrypt("secret")
	require.NoError(t, err)

	// Tamper with the ciphertext
	decoded, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
	decoded[len(decoded)-1] ^= 0xff // flip last byte
	tampered := base64.StdEncoding.EncodeToString(decoded)

	_, err = svc.Decrypt(tampered)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

func TestDecrypt_WrongKey(t *testing.T) {
	svc1, err := NewCryptoService(testKey)
	require.NoError(t, err)

	differentKey := "abcdefghijklmnopqrstuvwxyz012345" // different 32-byte key
	svc2, err := NewCryptoService(differentKey)
	require.NoError(t, err)

	encrypted, err := svc1.Encrypt("secret data")
	require.NoError(t, err)

	_, err = svc2.Decrypt(encrypted)
	assert.Error(t, err)
}

func TestMaskCredential(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", "****"},
		{"1 char", "a", "****"},
		{"2 chars", "ab", "****"},
		{"3 chars", "abc", "****"},
		{"4 chars", "abcd", "****"},
		{"5 chars", "abcde", "*bcde"},
		{"8 chars", "12345678", "****5678"},
		{"long string", "my-super-secret-api-key", "*******************-key"},
		{"api key example", "sk_live_abc123xyz", "*************3xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskCredential(tt.input)
			assert.Equal(t, tt.expected, result)

			// Verify last 4 chars are preserved for strings > 4
			if len(tt.input) > 4 {
				assert.True(t, strings.HasSuffix(result, tt.input[len(tt.input)-4:]))
				assert.Equal(t, len(tt.input), len(result))
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
