package audit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	svc := NewService(nil, nil)
	require.NotNil(t, svc)
	assert.Nil(t, svc.db)
	assert.Nil(t, svc.logger)
}

func TestMaskSensitiveFields_NilInput(t *testing.T) {
	result := maskSensitiveFields(nil)
	assert.Nil(t, result)
}

func TestMaskSensitiveFields_EmptyInput(t *testing.T) {
	result := maskSensitiveFields(map[string]interface{}{})
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestMaskSensitiveFields_NoSensitiveFields(t *testing.T) {
	fields := map[string]interface{}{
		"name":        "example.com",
		"status":      "active",
		"domain_name": "test.org",
		"expiry_date": "2025-12-01",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "example.com", result["name"])
	assert.Equal(t, "active", result["status"])
	assert.Equal(t, "test.org", result["domain_name"])
	assert.Equal(t, "2025-12-01", result["expiry_date"])
}

func TestMaskSensitiveFields_PasswordMasked(t *testing.T) {
	fields := map[string]interface{}{
		"username": "admin",
		"password": "super-secret-123",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "admin", result["username"])
	assert.Equal(t, "******", result["password"])
}

func TestMaskSensitiveFields_SecretMasked(t *testing.T) {
	fields := map[string]interface{}{
		"api_secret":    "abc123def456",
		"client_secret": "xyz789",
		"name":          "my-registrar",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "******", result["api_secret"])
	assert.Equal(t, "******", result["client_secret"])
	assert.Equal(t, "my-registrar", result["name"])
}

func TestMaskSensitiveFields_KeyMasked(t *testing.T) {
	fields := map[string]interface{}{
		"api_key":     "key-value-here",
		"access_key":  "AKIAIOSFODNN7EXAMPLE",
		"display_name": "Test Account",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "******", result["api_key"])
	assert.Equal(t, "******", result["access_key"])
	assert.Equal(t, "Test Account", result["display_name"])
}

func TestMaskSensitiveFields_TokenMasked(t *testing.T) {
	fields := map[string]interface{}{
		"auth_token":    "eyJhbGciOiJIUzI1NiJ9...",
		"refresh_token": "refresh-xyz",
		"channel_name":  "email-alerts",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "******", result["auth_token"])
	assert.Equal(t, "******", result["refresh_token"])
	assert.Equal(t, "email-alerts", result["channel_name"])
}

func TestMaskSensitiveFields_CredentialMasked(t *testing.T) {
	fields := map[string]interface{}{
		"credentials":        `{"user": "test", "pass": "abc"}`,
		"credential_data":    "encrypted-blob",
		"registrar_type":     "godaddy",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "******", result["credentials"])
	assert.Equal(t, "******", result["credential_data"])
	assert.Equal(t, "godaddy", result["registrar_type"])
}

func TestMaskSensitiveFields_CaseInsensitive(t *testing.T) {
	fields := map[string]interface{}{
		"PASSWORD":     "upper-case",
		"Api_Key":      "mixed-case",
		"AUTH_TOKEN":   "all-caps",
		"clientSecret": "camel-case",
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, "******", result["PASSWORD"])
	assert.Equal(t, "******", result["Api_Key"])
	assert.Equal(t, "******", result["AUTH_TOKEN"])
	assert.Equal(t, "******", result["clientSecret"])
}

func TestMaskSensitiveFields_PreservesNonStringValues(t *testing.T) {
	fields := map[string]interface{}{
		"domain_count": 42,
		"auto_renew":   true,
		"interval":     3.14,
	}

	result := maskSensitiveFields(fields)

	assert.Equal(t, 42, result["domain_count"])
	assert.Equal(t, true, result["auto_renew"])
	assert.Equal(t, 3.14, result["interval"])
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"password", true},
		{"user_password", true},
		{"PASSWORD", true},
		{"secret", true},
		{"api_secret", true},
		{"client_secret", true},
		{"key", true},
		{"api_key", true},
		{"access_key_id", true},
		{"token", true},
		{"auth_token", true},
		{"refresh_token", true},
		{"credential", true},
		{"credentials_encrypted", true},
		{"username", false},
		{"name", false},
		{"email", false},
		{"domain_name", false},
		{"status", false},
		{"created_at", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			assert.Equal(t, tt.expected, isSensitiveKey(tt.key))
		})
	}
}

func TestRetentionPeriod(t *testing.T) {
	expected := 365 * 24 * time.Hour
	assert.Equal(t, expected, RetentionPeriod)
}
