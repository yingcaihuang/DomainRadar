package adapter

import (
	"errors"
	"fmt"
)

const (
	// MaxCredentialLength is the maximum allowed length for credential values.
	MaxCredentialLength = 512

	// MaxAccountsPerType is the maximum number of accounts allowed per registrar type.
	MaxAccountsPerType = 20
)

var (
	ErrEmptyRegistrarType    = errors.New("registrar_type must not be empty")
	ErrEmptyAccountName      = errors.New("account_name must not be empty")
	ErrEmptyCredentials      = errors.New("credentials must not be empty")
	ErrCredentialsTooLong    = fmt.Errorf("credentials must not exceed %d characters", MaxCredentialLength)
	ErrMaxAccountsExceeded   = fmt.Errorf("maximum of %d accounts per registrar type exceeded", MaxAccountsPerType)
)

// ValidateRegistrarConfig validates registrar configuration fields.
// It checks that all required fields are non-empty, credentials do not exceed
// 512 characters, and the existing account count is below the limit of 20.
func ValidateRegistrarConfig(registrarType, accountName, credentials string, existingAccountCount int) error {
	if registrarType == "" {
		return ErrEmptyRegistrarType
	}
	if accountName == "" {
		return ErrEmptyAccountName
	}
	if credentials == "" {
		return ErrEmptyCredentials
	}
	if len(credentials) > MaxCredentialLength {
		return ErrCredentialsTooLong
	}
	if existingAccountCount >= MaxAccountsPerType {
		return ErrMaxAccountsExceeded
	}
	return nil
}
