package adapter

import (
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// nonEmptyString generates a non-empty string of at least 1 character.
func nonEmptyString() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.RuneFrom(nil, unicode.PrintRanges...), 1, 100, -1)
}

// credentialWithinLimit generates a non-empty credential string with length ≤ 512.
func credentialWithinLimit() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.RuneFrom(nil, unicode.PrintRanges...), 1, MaxCredentialLength, -1)
}

// credentialExceedingLimit generates a credential string with length > 512.
func credentialExceedingLimit() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.RuneFrom(nil, unicode.PrintRanges...), MaxCredentialLength+1, MaxCredentialLength+500, -1)
}

// TestProperty29_ValidConfigAccepted verifies that registrar configurations with all
// required fields non-empty, credentials ≤ 512 chars, and existingAccountCount < 20
// are accepted.
//
// **Validates: Requirements 11.4, 11.8**
func TestProperty29_ValidConfigAccepted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		registrarType := nonEmptyString().Draw(t, "registrar_type")
		accountName := nonEmptyString().Draw(t, "account_name")
		credentials := credentialWithinLimit().Draw(t, "credentials")
		existingCount := rapid.IntRange(0, MaxAccountsPerType-1).Draw(t, "existing_count")

		err := ValidateRegistrarConfig(registrarType, accountName, credentials, existingCount)
		if err != nil {
			t.Fatalf("expected valid config to be accepted, got error: %v\n"+
				"registrar_type=%q, account_name=%q, credentials_len=%d, existing_count=%d",
				err, registrarType, accountName, len(credentials), existingCount)
		}
	})
}

// TestProperty29_EmptyFieldsRejected verifies that configs with any empty required
// field are rejected.
//
// **Validates: Requirements 11.4, 11.8**
func TestProperty29_EmptyFieldsRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Choose which field to make empty (0=registrarType, 1=accountName, 2=credentials)
		emptyField := rapid.IntRange(0, 2).Draw(t, "empty_field")

		registrarType := nonEmptyString().Draw(t, "registrar_type")
		accountName := nonEmptyString().Draw(t, "account_name")
		credentials := credentialWithinLimit().Draw(t, "credentials")
		existingCount := rapid.IntRange(0, MaxAccountsPerType-1).Draw(t, "existing_count")

		switch emptyField {
		case 0:
			registrarType = ""
		case 1:
			accountName = ""
		case 2:
			credentials = ""
		}

		err := ValidateRegistrarConfig(registrarType, accountName, credentials, existingCount)
		if err == nil {
			t.Fatalf("expected rejection for empty field index %d, but got nil error", emptyField)
		}
	})
}

// TestProperty29_CredentialsTooLongRejected verifies that configs with credentials
// exceeding 512 characters are rejected.
//
// **Validates: Requirements 11.4, 11.8**
func TestProperty29_CredentialsTooLongRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		registrarType := nonEmptyString().Draw(t, "registrar_type")
		accountName := nonEmptyString().Draw(t, "account_name")
		credentials := credentialExceedingLimit().Draw(t, "credentials")
		existingCount := rapid.IntRange(0, MaxAccountsPerType-1).Draw(t, "existing_count")

		err := ValidateRegistrarConfig(registrarType, accountName, credentials, existingCount)
		if err == nil {
			t.Fatalf("expected rejection for credentials of length %d (> %d), but got nil error",
				len(credentials), MaxCredentialLength)
		}
		if err != ErrCredentialsTooLong {
			t.Fatalf("expected ErrCredentialsTooLong, got: %v", err)
		}
	})
}

// TestProperty29_MaxAccountsExceededRejected verifies that configs with
// existingAccountCount >= 20 are rejected.
//
// **Validates: Requirements 11.4, 11.8**
func TestProperty29_MaxAccountsExceededRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		registrarType := nonEmptyString().Draw(t, "registrar_type")
		accountName := nonEmptyString().Draw(t, "account_name")
		credentials := credentialWithinLimit().Draw(t, "credentials")
		existingCount := rapid.IntRange(MaxAccountsPerType, MaxAccountsPerType+100).Draw(t, "existing_count")

		err := ValidateRegistrarConfig(registrarType, accountName, credentials, existingCount)
		if err == nil {
			t.Fatalf("expected rejection for existing_count=%d (>= %d), but got nil error",
				existingCount, MaxAccountsPerType)
		}
		if err != ErrMaxAccountsExceeded {
			t.Fatalf("expected ErrMaxAccountsExceeded, got: %v", err)
		}
	})
}

// TestProperty29_AcceptedIffAllConditionsMet verifies the biconditional: a config is
// accepted if and only if ALL conditions are simultaneously satisfied (non-empty fields,
// credentials ≤ 512 chars, account count < 20).
//
// **Validates: Requirements 11.4, 11.8**
func TestProperty29_AcceptedIffAllConditionsMet(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate fields that may or may not be valid
		registrarType := rapid.OneOf(
			rapid.Just(""),
			nonEmptyString(),
		).Draw(t, "registrar_type")

		accountName := rapid.OneOf(
			rapid.Just(""),
			nonEmptyString(),
		).Draw(t, "account_name")

		// Generate credentials: empty, within limit, or exceeding limit
		credentials := rapid.OneOf(
			rapid.Just(""),
			credentialWithinLimit(),
			rapid.Just(strings.Repeat("x", MaxCredentialLength+1)),
		).Draw(t, "credentials")

		existingCount := rapid.IntRange(0, MaxAccountsPerType+10).Draw(t, "existing_count")

		// Determine expected validity
		allFieldsNonEmpty := registrarType != "" && accountName != "" && credentials != ""
		credentialsWithinLimit := len(credentials) <= MaxCredentialLength
		accountCountWithinLimit := existingCount < MaxAccountsPerType

		shouldAccept := allFieldsNonEmpty && credentialsWithinLimit && accountCountWithinLimit

		err := ValidateRegistrarConfig(registrarType, accountName, credentials, existingCount)
		accepted := err == nil

		if accepted != shouldAccept {
			t.Fatalf("biconditional violated: accepted=%v, shouldAccept=%v\n"+
				"registrar_type=%q (empty=%v), account_name=%q (empty=%v), "+
				"credentials_len=%d (<=512=%v), existing_count=%d (<20=%v), err=%v",
				accepted, shouldAccept,
				registrarType, registrarType == "",
				accountName, accountName == "",
				len(credentials), credentialsWithinLimit,
				existingCount, accountCountWithinLimit,
				err)
		}
	})
}
