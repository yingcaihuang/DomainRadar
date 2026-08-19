package adapter

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"domainradar/internal/domain"
)

// mockAdapter is a test implementation of RegistrarAdapter.
type mockAdapter struct {
	registrarType string
	domains       []domain.NormalizedDomain
	testConnErr   error
}

func (m *mockAdapter) ListDomains(_ context.Context, _ *RegistrarCredential) ([]domain.NormalizedDomain, error) {
	return m.domains, nil
}

func (m *mockAdapter) GetDomainDetail(_ context.Context, _ *RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	for i := range m.domains {
		if m.domains[i].DomainName == domainName {
			return &m.domains[i], nil
		}
	}
	return nil, nil
}

func (m *mockAdapter) TestConnection(_ context.Context, _ *RegistrarCredential) error {
	return m.testConnErr
}

func (m *mockAdapter) RegistrarType() string {
	return m.registrarType
}

func TestNewAdapterRegistry(t *testing.T) {
	registry := NewAdapterRegistry()
	assert.NotNil(t, registry)
	assert.Empty(t, registry.ListTypes())
}

func TestAdapterRegistry_Register(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &mockAdapter{registrarType: "godaddy"}

	registry.Register(adapter)

	types := registry.ListTypes()
	assert.Len(t, types, 1)
	assert.Contains(t, types, "godaddy")
}

func TestAdapterRegistry_Register_OverwritesSameType(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter1 := &mockAdapter{registrarType: "godaddy"}
	adapter2 := &mockAdapter{registrarType: "godaddy"}

	registry.Register(adapter1)
	registry.Register(adapter2)

	types := registry.ListTypes()
	assert.Len(t, types, 1)

	got, err := registry.Get("godaddy")
	require.NoError(t, err)
	// The second registered adapter should be returned
	assert.Same(t, adapter2, got.(*mockAdapter))
}

func TestAdapterRegistry_Register_MultipleTypes(t *testing.T) {
	registry := NewAdapterRegistry()
	registry.Register(&mockAdapter{registrarType: "godaddy"})
	registry.Register(&mockAdapter{registrarType: "cloudflare"})
	registry.Register(&mockAdapter{registrarType: "alibaba"})

	types := registry.ListTypes()
	sort.Strings(types)
	assert.Equal(t, []string{"alibaba", "cloudflare", "godaddy"}, types)
}

func TestAdapterRegistry_Get_Found(t *testing.T) {
	registry := NewAdapterRegistry()
	adapter := &mockAdapter{registrarType: "cloudflare"}
	registry.Register(adapter)

	got, err := registry.Get("cloudflare")
	require.NoError(t, err)
	assert.Equal(t, "cloudflare", got.RegistrarType())
}

func TestAdapterRegistry_Get_NotFound(t *testing.T) {
	registry := NewAdapterRegistry()

	got, err := registry.Get("nonexistent")
	assert.Nil(t, got)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported registrar type: nonexistent")
}

func TestAdapterRegistry_ListTypes_Empty(t *testing.T) {
	registry := NewAdapterRegistry()
	types := registry.ListTypes()
	assert.Empty(t, types)
}

func TestRegistrarCredential_Fields(t *testing.T) {
	cred := &RegistrarCredential{
		APIKey:         "key123",
		APISecret:      "secret456",
		Token:          "tok789",
		Username:       "user",
		AccessKeyID:    "AKID",
		SecretAccessKey: "SAK",
		IPWhitelist:    "1.2.3.4",
		Extra: map[string]string{
			"custom_field": "custom_value",
		},
	}

	assert.Equal(t, "key123", cred.APIKey)
	assert.Equal(t, "secret456", cred.APISecret)
	assert.Equal(t, "tok789", cred.Token)
	assert.Equal(t, "user", cred.Username)
	assert.Equal(t, "AKID", cred.AccessKeyID)
	assert.Equal(t, "SAK", cred.SecretAccessKey)
	assert.Equal(t, "1.2.3.4", cred.IPWhitelist)
	assert.Equal(t, "custom_value", cred.Extra["custom_field"])
}

func TestAdapterRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewAdapterRegistry()

	// Register adapters concurrently
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			registry.Register(&mockAdapter{registrarType: "type-a"})
		}
		close(done)
	}()

	// Read concurrently
	for i := 0; i < 100; i++ {
		_, _ = registry.Get("type-a")
		_ = registry.ListTypes()
	}

	<-done

	// Should still work correctly after concurrent access
	got, err := registry.Get("type-a")
	require.NoError(t, err)
	assert.Equal(t, "type-a", got.RegistrarType())
}
