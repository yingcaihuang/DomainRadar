package crypto

import (
	"encoding/base64"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

const propertyTestKey = "01234567890123456789012345678901" // exactly 32 bytes

// TestProperty7_CredentialEncryptionRoundTrip verifies that for any random UTF-8 string,
// Encrypt then Decrypt returns the original plaintext, and the base64-decoded ciphertext
// never contains the plaintext as a substring.
//
// **Validates: Requirements 11.2**
func TestProperty7_CredentialEncryptionRoundTrip(t *testing.T) {
	svc, err := NewCryptoService(propertyTestKey)
	if err != nil {
		t.Fatalf("failed to create crypto service: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		// Generate a random UTF-8 string using the default String generator
		plaintext := rapid.String().Draw(t, "plaintext")

		// Encrypt the plaintext
		encrypted, err := svc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		// Property: Decrypt(Encrypt(plaintext)) == plaintext
		decrypted, err := svc.Decrypt(encrypted)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}
		if decrypted != plaintext {
			t.Fatalf("round-trip failed: got %q, want %q", decrypted, plaintext)
		}

		// Property: base64-decoded ciphertext never contains plaintext as substring.
		// This is meaningful for credential-length strings (> 4 bytes), as very short
		// byte sequences will naturally appear in any random byte output.
		if len(plaintext) > 4 {
			decoded, err := base64.StdEncoding.DecodeString(encrypted)
			if err != nil {
				t.Fatalf("failed to base64 decode ciphertext: %v", err)
			}
			if strings.Contains(string(decoded), plaintext) {
				t.Fatalf("ciphertext contains plaintext %q as substring", plaintext)
			}
		}
	})
}

// TestProperty8_CredentialMaskingFormat verifies that for any string of length N>4,
// MaskCredential produces (N-4) asterisks followed by the last 4 characters of the input;
// for N≤4, it produces "****".
//
// **Validates: Requirements 11.3**
func TestProperty8_CredentialMaskingFormat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random string (printable ASCII to make assertions clearer)
		value := rapid.String().Draw(t, "value")
		n := len(value)

		result := MaskCredential(value)

		if n <= 4 {
			// Property: for strings of length ≤ 4, output is "****"
			if result != "****" {
				t.Fatalf("expected \"****\" for input of length %d, got %q", n, result)
			}
		} else {
			// Property: output length equals input length
			if len(result) != n {
				t.Fatalf("expected result length %d, got %d (result: %q)", n, len(result), result)
			}

			// Property: first (N-4) characters are all asterisks
			prefix := result[:n-4]
			expectedPrefix := strings.Repeat("*", n-4)
			if prefix != expectedPrefix {
				t.Fatalf("expected prefix of %d asterisks, got %q", n-4, prefix)
			}

			// Property: last 4 characters match the last 4 of the input
			suffix := result[n-4:]
			expectedSuffix := value[n-4:]
			if suffix != expectedSuffix {
				t.Fatalf("expected suffix %q, got %q", expectedSuffix, suffix)
			}
		}
	})
}
