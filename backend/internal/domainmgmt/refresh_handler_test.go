package domainmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"domainradar/internal/adapter"
	"domainradar/internal/audit"
	"domainradar/internal/auth"
	"domainradar/internal/crypto"
	"domainradar/internal/domain"
)

type refreshAdapter struct {
	registrarType string
	domains       []domain.NormalizedDomain
}

func (m *refreshAdapter) RegistrarType() string { return m.registrarType }
func (m *refreshAdapter) TestConnection(context.Context, *adapter.RegistrarCredential) error {
	return nil
}
func (m *refreshAdapter) ListDomains(context.Context, *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	return m.domains, nil
}
func (m *refreshAdapter) GetDomainDetail(_ context.Context, _ *adapter.RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	for i := range m.domains {
		if m.domains[i].DomainName == domainName {
			return &m.domains[i], nil
		}
	}
	return nil, nil
}

type refreshSetup struct {
	db     *gorm.DB
	router *gin.Engine
}

func newRefreshSetup(t *testing.T, registry *adapter.AdapterRegistry) *refreshSetup {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&domain.NormalizedDomain{},
		&domain.RegistrarConfig{},
		&domain.RegistrarAccount{},
		&domain.AuditLog{},
	))

	cryptoSvc, err := crypto.NewCryptoService("01234567890123456789012345678901")
	require.NoError(t, err)

	handler := NewDomainHandler(db, audit.NewService(db, zap.NewNop()), zap.NewNop(),
		WithAdapterRegistry(registry),
		WithCryptoService(cryptoSvc),
		WithWhoisScheduler(NewWhoisScheduler(db, zap.NewNop())),
	)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user", &auth.UserInfo{ID: 1, Email: "admin@test.com", Roles: []string{"admin"}})
		c.Next()
	})
	router.POST("/api/v1/domains/:id/refresh", handler.RefreshDomain)

	return &refreshSetup{db: db, router: router}
}

func TestRefreshDomain_FromRegistrar(t *testing.T) {
	// Adapter reports the current registrar truth: the domain was renewed.
	renewed := time.Now().Add(340 * 24 * time.Hour)
	registry := adapter.NewAdapterRegistry()
	registry.Register(&refreshAdapter{
		registrarType: "godaddy",
		domains: []domain.NormalizedDomain{
			{DomainName: "ouzuvideo.com", ExpirationDate: &renewed, Status: "active", AutoRenew: true},
		},
	})
	ts := newRefreshSetup(t, registry)

	encrypted, err := crypto.NewCryptoService("01234567890123456789012345678901")
	require.NoError(t, err)
	encCreds, err := encrypted.Encrypt(`{"token":"gd-token"}`)
	require.NoError(t, err)
	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	require.NoError(t, ts.db.Create(&config).Error)
	account := domain.RegistrarAccount{
		RegistrarConfigID: config.ID, AccountName: "gd",
		CredentialsEncrypted: encCreds, Status: "connected",
	}
	require.NoError(t, ts.db.Create(&account).Error)

	stale := time.Now().Add(-27 * 24 * time.Hour)
	d := domain.NormalizedDomain{
		DomainName: "ouzuvideo.com", RegistrarAccountID: &account.ID,
		ExpirationDate: &stale, Status: "expired",
	}
	require.NoError(t, ts.db.Create(&d).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/domains/1/refresh", nil)
	ts.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data   domain.NormalizedDomain `json:"data"`
		Source string                  `json:"source"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "registrar", resp.Source)
	assert.Equal(t, "active", resp.Data.Status)
	require.NotNil(t, resp.Data.ExpirationDate)
	assert.WithinDuration(t, renewed, *resp.Data.ExpirationDate, time.Second)
	assert.NotNil(t, resp.Data.LastSyncAt)
}

func TestRefreshDomain_FromWhois(t *testing.T) {
	ts := newRefreshSetup(t, adapter.NewAdapterRegistry())

	// Manual domain: WHOIS reports the domain expired.
	past := time.Now().Add(-9 * 24 * time.Hour).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"domain":"manual.com","dates":{"expires":"` + past + `"}}`))
	}))
	defer server.Close()
	t.Setenv("WHO_DAT_URL", server.URL)

	d := domain.NormalizedDomain{DomainName: "manual.com", Status: "active"}
	require.NoError(t, ts.db.Create(&d).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/domains/1/refresh", nil)
	ts.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data   domain.NormalizedDomain `json:"data"`
		Source string                  `json:"source"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "whois", resp.Source)
	assert.Equal(t, "expired", resp.Data.Status)
}

func TestRefreshDomain_DomainNotFound(t *testing.T) {
	ts := newRefreshSetup(t, adapter.NewAdapterRegistry())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/domains/999/refresh", nil)
	ts.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
