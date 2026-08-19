package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"domainradar/internal/audit"
	"domainradar/internal/auth"
	"domainradar/internal/crypto"
	"domainradar/internal/domain"
)

// testSetup holds shared test dependencies.
type testSetup struct {
	db              *gorm.DB
	handler         *RegistrarHandler
	router          *gin.Engine
	cryptoService   *crypto.CryptoService
	adapterRegistry *AdapterRegistry
	syncTrigger     *mockSyncTrigger
}

// newTestSetup creates a complete test environment with SQLite in-memory database.
func newTestSetup(t *testing.T) *testSetup {
	t.Helper()

	// Use SQLite in-memory for tests
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate test models
	err = db.AutoMigrate(
		&domain.RegistrarConfig{},
		&domain.RegistrarAccount{},
		&domain.SyncLog{},
		&domain.AuditLog{},
	)
	require.NoError(t, err)

	// Set up crypto service with a 32-byte test key
	masterKey := "01234567890123456789012345678901"
	cryptoSvc, err := crypto.NewCryptoService(masterKey)
	require.NoError(t, err)

	// Set up adapter registry with a mock adapter
	registry := NewAdapterRegistry()
	registry.Register(&mockAdapter{registrarType: "godaddy"})
	registry.Register(&mockAdapter{registrarType: "cloudflare"})

	logger := zap.NewNop()
	auditSvc := audit.NewService(db, logger)

	syncMock := &mockSyncTrigger{}

	handler := NewRegistrarHandler(db, cryptoSvc, registry, auditSvc, logger, WithSyncTrigger(syncMock))

	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add mock auth middleware that sets user context
	router.Use(func(c *gin.Context) {
		c.Set("user", &auth.UserInfo{
			ID:    1,
			Email: "admin@test.com",
			Roles: []string{"admin"},
		})
		c.Next()
	})

	// Register routes (without actual permission middleware for testing)
	v1 := router.Group("/api/v1")
	registrars := v1.Group("/registrars")
	{
		registrars.GET("", handler.ListRegistrars)
		registrars.POST("", handler.CreateRegistrar)
		registrars.PUT("/:id", handler.UpdateRegistrar)
		registrars.DELETE("/:id", handler.DeleteRegistrar)
		registrars.POST("/:id/test", handler.TestConnection)
		registrars.POST("/:id/sync", handler.TriggerSync)
		registrars.GET("/:id/status", handler.GetStatus)
	}

	return &testSetup{
		db:              db,
		handler:         handler,
		router:          router,
		cryptoService:   cryptoSvc,
		adapterRegistry: registry,
		syncTrigger:     syncMock,
	}
}

func TestListRegistrars_Empty(t *testing.T) {
	ts := newTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/registrars", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]RegistrarAccountResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp["data"])
}

func TestCreateRegistrar_Success(t *testing.T) {
	ts := newTestSetup(t)

	body := CreateRegistrarRequest{
		RegistrarType: "godaddy",
		DisplayName:   "GoDaddy",
		AccountName:   "My GoDaddy Account",
		Credentials: map[string]string{
			"api_key":    "test-key-12345678",
			"api_secret": "test-secret-abcdef",
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]*RegistrarAccountResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, "My GoDaddy Account", data.AccountName)
	assert.Equal(t, "godaddy", data.RegistrarType)
	assert.Equal(t, "GoDaddy", data.DisplayName)
	assert.Equal(t, "disconnected", data.Status)

	// Verify credentials are masked (last 4 chars visible)
	// "test-key-12345678" = 17 chars → 13 asterisks + "5678"
	assert.Equal(t, "*************5678", data.Credentials["api_key"])
	// "test-secret-abcdef" = 18 chars → 14 asterisks + "cdef"
	assert.Equal(t, "**************cdef", data.Credentials["api_secret"])
}

func TestCreateRegistrar_MissingRequiredFields(t *testing.T) {
	ts := newTestSetup(t)

	tests := []struct {
		name string
		body CreateRegistrarRequest
	}{
		{
			name: "missing registrar_type",
			body: CreateRegistrarRequest{
				DisplayName: "Test",
				AccountName: "Test Account",
				Credentials: map[string]string{"api_key": "key"},
			},
		},
		{
			name: "missing display_name",
			body: CreateRegistrarRequest{
				RegistrarType: "godaddy",
				AccountName:   "Test Account",
				Credentials:   map[string]string{"api_key": "key"},
			},
		},
		{
			name: "missing account_name",
			body: CreateRegistrarRequest{
				RegistrarType: "godaddy",
				DisplayName:   "Test",
				Credentials:   map[string]string{"api_key": "key"},
			},
		},
		{
			name: "empty credentials",
			body: CreateRegistrarRequest{
				RegistrarType: "godaddy",
				DisplayName:   "Test",
				AccountName:   "Test Account",
				Credentials:   map[string]string{},
			},
		},
		{
			name: "empty credential value",
			body: CreateRegistrarRequest{
				RegistrarType: "godaddy",
				DisplayName:   "Test",
				AccountName:   "Test Account",
				Credentials:   map[string]string{"api_key": ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/v1/registrars", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			ts.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestCreateRegistrar_CredentialsTooLong(t *testing.T) {
	ts := newTestSetup(t)

	// Create a credential value that exceeds 512 characters
	longValue := make([]byte, 513)
	for i := range longValue {
		longValue[i] = 'a'
	}

	body := CreateRegistrarRequest{
		RegistrarType: "godaddy",
		DisplayName:   "GoDaddy",
		AccountName:   "Test Account",
		Credentials: map[string]string{
			"api_key": string(longValue),
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateRegistrar_UnsupportedType(t *testing.T) {
	ts := newTestSetup(t)

	body := CreateRegistrarRequest{
		RegistrarType: "unsupported_registrar",
		DisplayName:   "Unknown",
		AccountName:   "Test Account",
		Credentials:   map[string]string{"api_key": "key12345"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateRegistrar_MaxAccountsPerType(t *testing.T) {
	ts := newTestSetup(t)

	// Create a config first
	config := domain.RegistrarConfig{
		RegistrarType: "godaddy",
		DisplayName:   "GoDaddy",
	}
	ts.db.Create(&config)

	// Create 20 accounts for the same type
	for i := 0; i < 20; i++ {
		encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"key"}`)
		acct := domain.RegistrarAccount{
			RegistrarConfigID:    config.ID,
			AccountName:          fmt.Sprintf("Account %d", i),
			CredentialsEncrypted: encrypted,
			Status:               "disconnected",
		}
		ts.db.Create(&acct)
	}

	// Attempt to create a 21st account
	body := CreateRegistrarRequest{
		RegistrarType: "godaddy",
		DisplayName:   "GoDaddy",
		AccountName:   "Account 21",
		Credentials:   map[string]string{"api_key": "key12345"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateRegistrar_Success(t *testing.T) {
	ts := newTestSetup(t)

	// Create an account first
	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"old-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Original Name",
		CredentialsEncrypted: encrypted,
		Status:               "disconnected",
	}
	ts.db.Create(&account)

	// Update account name and credentials
	body := UpdateRegistrarRequest{
		AccountName: "Updated Name",
		Credentials: map[string]string{
			"api_key": "new-key-56789abc",
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v1/registrars/%d", account.ID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]*RegistrarAccountResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, "Updated Name", data.AccountName)
	// New credentials should be masked
	assert.Equal(t, "************9abc", data.Credentials["api_key"])
}

func TestUpdateRegistrar_NotFound(t *testing.T) {
	ts := newTestSetup(t)

	body := UpdateRegistrarRequest{AccountName: "New Name"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/v1/registrars/999", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteRegistrar_Success(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"key12345"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "To Delete",
		CredentialsEncrypted: encrypted,
		Status:               "disconnected",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v1/registrars/%d", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify it's deleted
	var found domain.RegistrarAccount
	result := ts.db.First(&found, account.ID)
	assert.Error(t, result.Error)
}

func TestDeleteRegistrar_NotFound(t *testing.T) {
	ts := newTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/v1/registrars/999", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTestConnection_Success(t *testing.T) {
	ts := newTestSetup(t)

	// Register a mock adapter that succeeds
	ts.adapterRegistry.Register(&mockAdapter{
		registrarType: "godaddy",
		testConnErr:   nil,
	})

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"test-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Test Account",
		CredentialsEncrypted: encrypted,
		Status:               "disconnected",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/registrars/%d/test", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "connected", resp["status"])

	// Verify status was persisted
	var updated domain.RegistrarAccount
	ts.db.First(&updated, account.ID)
	assert.Equal(t, "connected", updated.Status)
}

func TestTestConnection_Failure(t *testing.T) {
	ts := newTestSetup(t)

	// Register a mock adapter that fails
	ts.adapterRegistry.Register(&mockAdapter{
		registrarType: "godaddy",
		testConnErr:   errors.New("connection refused"),
	})

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"test-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Test Account",
		CredentialsEncrypted: encrypted,
		Status:               "connected",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/registrars/%d/test", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "disconnected", resp["status"])
	assert.Equal(t, "connection refused", resp["message"])

	// Verify status was persisted
	var updated domain.RegistrarAccount
	ts.db.First(&updated, account.ID)
	assert.Equal(t, "disconnected", updated.Status)
}

func TestTestConnection_NotFound(t *testing.T) {
	ts := newTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars/999/test", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetStatus_Success(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"test-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Status Account",
		CredentialsEncrypted: encrypted,
		Status:               "connected",
		DomainCount:          42,
	}
	ts.db.Create(&account)

	// Create some sync error logs
	for i := 0; i < 3; i++ {
		log := domain.SyncLog{
			RegistrarAccountID: account.ID,
			Status:             "failed",
			ErrorMessage:       fmt.Sprintf("error %d", i),
		}
		ts.db.Create(&log)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/registrars/%d/status", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]*RegistrarStatusResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, "Status Account", data.AccountName)
	assert.Equal(t, "godaddy", data.RegistrarType)
	assert.Equal(t, "connected", data.Status)
	assert.Equal(t, 42, data.DomainCount)
	assert.Len(t, data.RecentErrors, 3)
}

func TestGetStatus_NotFound(t *testing.T) {
	ts := newTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/registrars/999/status", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListRegistrars_WithAccounts(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"key-abcd1234"}`)
	account1 := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Account 1",
		CredentialsEncrypted: encrypted,
		Status:               "connected",
	}
	ts.db.Create(&account1)

	encrypted2, _ := ts.cryptoService.Encrypt(`{"api_key":"key-efgh5678"}`)
	account2 := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Account 2",
		CredentialsEncrypted: encrypted2,
		Status:               "disconnected",
	}
	ts.db.Create(&account2)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/registrars", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]RegistrarAccountResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp["data"], 2)

	// Verify credentials are masked
	for _, acct := range resp["data"] {
		for _, v := range acct.Credentials {
			// Should end with 4 characters and have asterisks before
			assert.Contains(t, v, "*")
		}
	}
}

func TestCreateRegistrar_CredentialAt512CharsIsValid(t *testing.T) {
	ts := newTestSetup(t)

	// Create a credential value exactly at the 512-char limit
	exactValue := make([]byte, 512)
	for i := range exactValue {
		exactValue[i] = 'x'
	}

	body := CreateRegistrarRequest{
		RegistrarType: "godaddy",
		DisplayName:   "GoDaddy",
		AccountName:   "Boundary Account",
		Credentials: map[string]string{
			"api_key": string(exactValue),
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCredentialMaskingFormat(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	// Test with short credential (<=4 chars)
	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"abc","api_secret":"longersecretvalue"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Mask Test",
		CredentialsEncrypted: encrypted,
		Status:               "disconnected",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/registrars", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]RegistrarAccountResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp["data"], 1)

	// Short credential (3 chars "abc") should be "****"
	assert.Equal(t, "****", resp["data"][0].Credentials["api_key"])
	// Longer credential should show last 4 chars
	assert.Equal(t, "*************alue", resp["data"][0].Credentials["api_secret"])
}

// testMockAdapterWithError is used to simulate connection failures.
type testMockAdapterWithError struct {
	registrarType string
	testErr       error
}

func (m *testMockAdapterWithError) ListDomains(_ context.Context, _ *RegistrarCredential) ([]domain.NormalizedDomain, error) {
	return nil, nil
}

func (m *testMockAdapterWithError) GetDomainDetail(_ context.Context, _ *RegistrarCredential, _ string) (*domain.NormalizedDomain, error) {
	return nil, nil
}

func (m *testMockAdapterWithError) TestConnection(_ context.Context, _ *RegistrarCredential) error {
	return m.testErr
}

func (m *testMockAdapterWithError) RegistrarType() string {
	return m.registrarType
}

func TestUpdateRegistrar_CredentialsTooLong(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"old-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Account",
		CredentialsEncrypted: encrypted,
		Status:               "disconnected",
	}
	ts.db.Create(&account)

	longValue := make([]byte, 513)
	for i := range longValue {
		longValue[i] = 'a'
	}

	body := UpdateRegistrarRequest{
		Credentials: map[string]string{
			"api_key": string(longValue),
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v1/registrars/%d", account.ID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInvalidIDParameter(t *testing.T) {
	ts := newTestSetup(t)

	tests := []struct {
		method string
		path   string
	}{
		{"PUT", "/api/v1/registrars/invalid"},
		{"DELETE", "/api/v1/registrars/invalid"},
		{"POST", "/api/v1/registrars/invalid/test"},
		{"GET", "/api/v1/registrars/invalid/status"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tt.method, tt.path, nil)
			ts.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

// --- mockSyncTrigger for testing TriggerSync ---

// mockSyncTrigger implements SyncTrigger for testing purposes.
type mockSyncTrigger struct {
	callCount   atomic.Int32
	lastAccount atomic.Uint32
	err         error
}

func (m *mockSyncTrigger) RunSyncCycle(_ context.Context, accountID uint) error {
	m.callCount.Add(1)
	m.lastAccount.Store(uint32(accountID))
	return m.err
}

// --- TriggerSync tests ---

func TestTriggerSync_Success(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"test-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Sync Account",
		CredentialsEncrypted: encrypted,
		Status:               "connected",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/registrars/%d/sync", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "sync triggered", resp["message"])
	assert.Equal(t, float64(account.ID), resp["account_id"])

	// Give goroutine a moment to execute
	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, ts.syncTrigger.callCount.Load(), int32(1))
	assert.Equal(t, uint32(account.ID), ts.syncTrigger.lastAccount.Load())
}

func TestTriggerSync_AccountNotFound(t *testing.T) {
	ts := newTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars/999/sync", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTriggerSync_AccountDisconnected(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"test-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Disconnected Account",
		CredentialsEncrypted: encrypted,
		Status:               "disconnected",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/registrars/%d/sync", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	errorBody := resp["error"].(map[string]interface{})
	assert.Contains(t, errorBody["message"], "disconnected")
}

func TestTriggerSync_AccountInErrorStatus(t *testing.T) {
	ts := newTestSetup(t)

	config := domain.RegistrarConfig{RegistrarType: "godaddy", DisplayName: "GoDaddy"}
	ts.db.Create(&config)

	encrypted, _ := ts.cryptoService.Encrypt(`{"api_key":"test-key-1234"}`)
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          "Error Account",
		CredentialsEncrypted: encrypted,
		Status:               "error",
	}
	ts.db.Create(&account)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/registrars/%d/sync", account.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	errorBody := resp["error"].(map[string]interface{})
	assert.Contains(t, errorBody["message"], "error")
	assert.Contains(t, errorBody["message"], "must be connected")
}

func TestTriggerSync_InvalidID(t *testing.T) {
	ts := newTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/registrars/invalid/sync", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
