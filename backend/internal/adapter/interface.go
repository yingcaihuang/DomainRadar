package adapter

import (
	"context"
	"fmt"
	"sync"

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
	SecretAccessKey  string `json:"secret_access_key,omitempty"`
	IPWhitelist     string `json:"ip_whitelist,omitempty"`

	// Extra holds any additional registrar-specific credential fields.
	Extra map[string]string `json:"extra,omitempty"`
}

// RegistrarAdapter defines the contract for all registrar integrations.
type RegistrarAdapter interface {
	// ListDomains retrieves all active domains from the registrar account.
	ListDomains(ctx context.Context, credential *RegistrarCredential) ([]domain.NormalizedDomain, error)

	// GetDomainDetail retrieves detailed info for a specific domain.
	GetDomainDetail(ctx context.Context, credential *RegistrarCredential, domainName string) (*domain.NormalizedDomain, error)

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
