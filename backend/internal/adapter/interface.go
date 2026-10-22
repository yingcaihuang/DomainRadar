package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"domainradar/internal/domain"
)

// RegistrarCredential holds decrypted credential data for passing to adapters.
// It uses a flexible map-based approach to support various registrar authentication schemes.
type RegistrarCredential struct {
	// Common credential fields
	APIKey          string `json:"api_key,omitempty"`
	APISecret       string `json:"api_secret,omitempty"`
	Token           string `json:"token,omitempty"`
	Username        string `json:"username,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	IPWhitelist     string `json:"ip_whitelist,omitempty"`

	// Extra holds any additional registrar-specific credential fields.
	Extra map[string]string `json:"extra,omitempty"`
}

// ParseRegistrarCredential decodes the flat credential map stored by registrar handlers.
func ParseRegistrarCredential(plaintext string) (*RegistrarCredential, error) {
	var fields map[string]string
	if err := json.Unmarshal([]byte(plaintext), &fields); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("credentials must not be empty")
	}
	cred := &RegistrarCredential{
		APIKey:          fields["api_key"],
		APISecret:       fields["api_secret"],
		Token:           fields["token"],
		Username:        fields["username"],
		AccessKeyID:     fields["access_key_id"],
		SecretAccessKey: fields["secret_access_key"],
		IPWhitelist:     fields["ip_whitelist"],
	}
	for _, key := range []string{"api_key", "api_secret", "token", "username", "access_key_id", "secret_access_key", "ip_whitelist"} {
		delete(fields, key)
	}
	cred.Extra = fields
	return cred, nil
}

// DomainDetail is a partial registrar snapshot used by both list and detail APIs.
// Nil fields are unknown; false flags and empty non-nil lists are authoritative.
type DomainDetail struct {
	DomainName          string
	RegistrarIdentifier string
	DataSourceType      string
	LastSyncAt          *time.Time
	ExpirationDate      *time.Time
	CreationDate        *time.Time
	AutoRenew           *bool
	RenewalDeadline     *time.Time
	Status              string
	Nameservers         domain.JSON
	PrivacyProtection   *bool
	LockStatus          *bool
}

// NewDomainDetail copies common fields; adapters must explicitly supply known flags.
func NewDomainDetail(d domain.NormalizedDomain) *DomainDetail {
	return &DomainDetail{
		DomainName: d.DomainName, RegistrarIdentifier: d.RegistrarIdentifier,
		DataSourceType: d.DataSourceType, LastSyncAt: d.LastSyncAt,
		ExpirationDate: d.ExpirationDate, CreationDate: d.CreationDate,
		RenewalDeadline: d.RenewalDeadline, Status: d.Status, Nameservers: d.Nameservers,
	}
}

// UpdateFields returns only authoritative fields, preserving unknown local values.
func (d DomainDetail) UpdateFields() (map[string]interface{}, error) {
	updates := make(map[string]interface{})
	if d.ExpirationDate != nil {
		updates["expiration_date"] = d.ExpirationDate
	}
	if d.CreationDate != nil {
		updates["creation_date"] = d.CreationDate
	}
	if d.RenewalDeadline != nil {
		updates["renewal_deadline"] = d.RenewalDeadline
	}
	if d.AutoRenew != nil {
		updates["auto_renew"] = *d.AutoRenew
	}
	if d.PrivacyProtection != nil {
		updates["privacy_protection"] = *d.PrivacyProtection
	}
	if d.LockStatus != nil {
		updates["lock_status"] = *d.LockStatus
	}
	if d.Status != "" {
		updates["status"] = d.Status
	}
	if d.Nameservers != nil {
		value, err := d.Nameservers.Value()
		if err != nil {
			return nil, fmt.Errorf("invalid registrar nameservers: %w", err)
		}
		updates["nameservers"] = value
	}
	return updates, nil
}

// ToNormalizedDomain applies defaults only when creating a new local domain.
func (d DomainDetail) ToNormalizedDomain() domain.NormalizedDomain {
	return domain.NormalizedDomain{
		DomainName: d.DomainName, RegistrarIdentifier: d.RegistrarIdentifier,
		DataSourceType: d.DataSourceType, LastSyncAt: d.LastSyncAt,
		ExpirationDate: d.ExpirationDate, CreationDate: d.CreationDate,
		RenewalDeadline: d.RenewalDeadline, Status: d.Status, Nameservers: d.Nameservers,
		AutoRenew:         d.AutoRenew != nil && *d.AutoRenew,
		PrivacyProtection: d.PrivacyProtection != nil && *d.PrivacyProtection,
		LockStatus:        d.LockStatus != nil && *d.LockStatus,
	}
}

// RegistrarAdapter defines the contract for all registrar integrations.
type RegistrarAdapter interface {
	// ListDomains retrieves all active domains from the registrar account.
	ListDomains(ctx context.Context, credential *RegistrarCredential) ([]DomainDetail, error)

	// GetDomainDetail retrieves detailed info for a specific domain.
	GetDomainDetail(ctx context.Context, credential *RegistrarCredential, domainName string) (*DomainDetail, error)

	// TestConnection validates that credentials are working.
	TestConnection(ctx context.Context, credential *RegistrarCredential) error

	// RegistrarType returns the identifier for this adapter (e.g., "godaddy", "cloudflare").
	RegistrarType() string
}

// AdapterRegistry manages all available registrar adapters.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]RegistrarAdapter
}

// NewAdapterRegistry creates a new AdapterRegistry.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		adapters: make(map[string]RegistrarAdapter),
	}
}

// Register adds an adapter to the registry, keyed by its RegistrarType().
func (r *AdapterRegistry) Register(adapter RegistrarAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[adapter.RegistrarType()] = adapter
}

// Get returns the adapter for the given registrar type, or an error if not found.
func (r *AdapterRegistry) Get(registrarType string) (RegistrarAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[registrarType]
	if !ok {
		return nil, fmt.Errorf("unsupported registrar type: %s", registrarType)
	}
	return adapter, nil
}

// ListTypes returns a list of all registered adapter type identifiers.
func (r *AdapterRegistry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.adapters))
	for t := range r.adapters {
		types = append(types, t)
	}
	return types
}
