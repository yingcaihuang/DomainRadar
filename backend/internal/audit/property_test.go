package audit

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 10.3**
// Property 30: Audit log completeness — For any CUD operation with authenticated user,
// the audit entry contains all required fields with credentials masked.
// Specifically: after maskSensitiveFields, all sensitive keys are masked to "******",
// all non-sensitive keys retain their original values, and the key set is preserved.
func TestProperty30_AuditLogCompleteness_MaskSensitiveFields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random field map with a mix of sensitive and non-sensitive keys.
		fields := generateFieldMap(t)

		// Apply the masking function.
		result := maskSensitiveFields(fields)

		// Property: output map has the same keys as the input.
		if len(result) != len(fields) {
			t.Fatalf("key count mismatch: input has %d keys, output has %d keys", len(fields), len(result))
		}

		for key, originalValue := range fields {
			maskedValue, exists := result[key]
			if !exists {
				t.Fatalf("key %q present in input but missing from output", key)
			}

			if isSensitiveKey(key) {
				// Property: all sensitive fields have value "******".
				if maskedValue != "******" {
					t.Fatalf("sensitive key %q was not masked: got %v, want \"******\"", key, maskedValue)
				}
			} else {
				// Property: all non-sensitive fields retain their original value.
				if maskedValue != originalValue {
					t.Fatalf("non-sensitive key %q was modified: got %v, want %v", key, maskedValue, originalValue)
				}
			}
		}

		// Property: no sensitive field leaks through unmasked.
		for key, value := range result {
			if isSensitiveKey(key) && value != "******" {
				t.Fatalf("sensitive key %q leaked unmasked value: %v", key, value)
			}
		}
	})
}

// generateFieldMap produces a random map[string]interface{} that includes both
// sensitive keys (containing one of the sensitive patterns) and non-sensitive keys.
func generateFieldMap(t *rapid.T) map[string]interface{} {
	// Generate at least 1 sensitive and 1 non-sensitive key to ensure coverage.
	numSensitive := rapid.IntRange(1, 5).Draw(t, "numSensitive")
	numNonSensitive := rapid.IntRange(1, 5).Draw(t, "numNonSensitive")

	fields := make(map[string]interface{}, numSensitive+numNonSensitive)

	// Generate sensitive keys.
	for i := 0; i < numSensitive; i++ {
		key := drawSensitiveKey(t, i)
		value := drawFieldValue(t, i)
		fields[key] = value
	}

	// Generate non-sensitive keys.
	for i := 0; i < numNonSensitive; i++ {
		key := drawNonSensitiveKey(t, i)
		value := drawFieldValue(t, i)
		fields[key] = value
	}

	return fields
}

// drawSensitiveKey generates a key that contains one of the sensitive patterns.
func drawSensitiveKey(t *rapid.T, index int) string {
	pattern := rapid.SampledFrom(sensitiveKeyPatterns).Draw(t, "sensitivePattern")

	// Optionally add a prefix and/or suffix to the pattern.
	prefixes := []string{"", "api_", "user_", "client_", "auth_", "db_", "smtp_"}
	suffixes := []string{"", "_value", "_data", "_hash", "_encrypted", "_id"}

	prefix := rapid.SampledFrom(prefixes).Draw(t, "prefix")
	suffix := rapid.SampledFrom(suffixes).Draw(t, "suffix")

	key := prefix + pattern + suffix

	// Optionally vary the casing.
	caseVariant := rapid.IntRange(0, 2).Draw(t, "caseVariant")
	switch caseVariant {
	case 1:
		key = strings.ToUpper(key)
	case 2:
		// Title-case the first letter only.
		if len(key) > 0 {
			key = strings.ToUpper(key[:1]) + key[1:]
		}
	}

	// Ensure uniqueness by appending index if needed (avoid map key collision).
	if index > 0 {
		key = key + rapid.StringMatching(`[a-z]{0,3}`).Draw(t, "keySuffix")
	}

	return key
}

// drawNonSensitiveKey generates a key guaranteed to NOT contain any sensitive patterns.
func drawNonSensitiveKey(t *rapid.T, index int) string {
	// Use a curated list of safe field names that don't contain any sensitive substrings.
	safeNames := []string{
		"name", "domain", "status", "registrar", "email",
		"expiry_date", "created_at", "updated_at", "auto_renew",
		"nameservers", "dns_zone", "account_name", "owner",
		"notes", "tags", "group", "registrar_type", "sync_interval",
		"health_score", "uptime", "response_time", "port",
		"hostname", "description", "category", "priority",
		"action_type", "resource_type", "resource_id", "user_id",
	}

	base := rapid.SampledFrom(safeNames).Draw(t, "nonSensitiveBase")

	// Ensure uniqueness by appending index.
	if index > 0 {
		base = base + rapid.StringMatching(`_[a-z]{1,3}`).Draw(t, "nonSensitiveSuffix")
	}

	// Verify it's truly not sensitive (safety check).
	if isSensitiveKey(base) {
		// Fallback to a guaranteed-safe name.
		base = "field_" + rapid.StringMatching(`[a-z]{3,6}`).Draw(t, "fallbackName")
	}

	return base
}

// drawFieldValue generates a random field value of varying types.
func drawFieldValue(t *rapid.T, index int) interface{} {
	valueType := rapid.IntRange(0, 3).Draw(t, "valueType")
	switch valueType {
	case 0:
		return rapid.String().Draw(t, "stringValue")
	case 1:
		return rapid.Int().Draw(t, "intValue")
	case 2:
		return rapid.Bool().Draw(t, "boolValue")
	default:
		return rapid.Float64().Draw(t, "floatValue")
	}
}
