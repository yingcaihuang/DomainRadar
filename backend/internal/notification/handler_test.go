package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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

// handlerTestSetup holds shared test dependencies for notification handler tests.
type handlerTestSetup struct {
	db              *gorm.DB
	handler         *NotificationHandler
	router          *gin.Engine
	cryptoService   *crypto.CryptoService
	channelRegistry *ChannelRegistry
}

// newHandlerTestSetup creates a complete test environment with SQLite in-memory database.
func newHandlerTestSetup(t *testing.T) *handlerTestSetup {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&domain.NotificationChannel{},
		&domain.NotificationRule{},
		&domain.NormalizedDomain{},
		&domain.AuditLog{},
	)
	require.NoError(t, err)

	masterKey := "01234567890123456789012345678901"
	cryptoSvc, err := crypto.NewCryptoService(masterKey)
	require.NoError(t, err)

	registry := NewChannelRegistry()
	registry.Register("webhook", func(config *ChannelConfig) NotificationChannel {
		return NewWebhookChannel(config)
	})
	registry.Register("email", func(config *ChannelConfig) NotificationChannel {
		return NewEmailChannel(config)
	})
	registry.Register("wechat_work", func(config *ChannelConfig) NotificationChannel {
		return NewWeChatWorkChannel(config)
	})

	logger := zap.NewNop()
	auditSvc := audit.NewService(db, logger)

	handler := NewNotificationHandler(db, cryptoSvc, registry, auditSvc, logger)

	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add mock auth middleware
	router.Use(func(c *gin.Context) {
		c.Set("user", &auth.UserInfo{
			ID:    1,
			Email: "admin@test.com",
			Roles: []string{"admin"},
		})
		c.Next()
	})

	// Register routes without permission middleware for testing
	v1 := router.Group("/api/v1/notifications")
	channels := v1.Group("/channels")
	{
		channels.GET("", handler.ListChannels)
		channels.POST("", handler.CreateChannel)
		channels.PUT("/:id", handler.UpdateChannel)
		channels.DELETE("/:id", handler.DeleteChannel)
		channels.POST("/:id/test", handler.TestChannel)
	}
	rules := v1.Group("/rules")
	{
		rules.GET("", handler.ListRules)
		rules.POST("", handler.CreateRule)
		rules.PUT("/:id", handler.UpdateRule)
		rules.DELETE("/:id", handler.DeleteRule)
	}

	return &handlerTestSetup{
		db:              db,
		handler:         handler,
		router:          router,
		cryptoService:   cryptoSvc,
		channelRegistry: registry,
	}
}

// --- Channel CRUD tests ---

func TestListChannels_Empty(t *testing.T) {
	ts := newHandlerTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/notifications/channels", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]ChannelResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp["data"])
}

func TestCreateChannel_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := CreateChannelRequest{
		ChannelType: "webhook",
		Name:        "My Webhook",
		Config: map[string]string{
			"url":     "https://hooks.example.com/alert",
			"headers": "Authorization:Bearer secrettoken123",
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/channels", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]*ChannelResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, "webhook", data.ChannelType)
	assert.Equal(t, "My Webhook", data.Name)
	assert.Equal(t, "inactive", data.Status)

	// Verify config values are masked
	// "https://hooks.example.com/alert" = 31 chars → 27 asterisks + "lert"
	assert.Equal(t, "***************************lert", data.Config["url"])
	assert.Contains(t, data.Config["headers"], "****")
}

func TestCreateChannel_UnsupportedType(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := CreateChannelRequest{
		ChannelType: "telegram",
		Name:        "My Telegram",
		Config:      map[string]string{"bot_token": "token123"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/channels", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateChannel_EmptyConfig(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := CreateChannelRequest{
		ChannelType: "webhook",
		Name:        "Empty Config",
		Config:      map[string]string{},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/channels", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateChannel_MissingFields(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Missing channel_type
	body := map[string]interface{}{
		"name":   "Test",
		"config": map[string]string{"url": "http://test.com"},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/channels", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateChannel_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Create a channel first
	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://old.example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Old Name",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	body := UpdateChannelRequest{
		Name: "New Name",
		Config: map[string]string{
			"url": "https://new.example.com/webhook",
		},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v1/notifications/channels/%d", channel.ID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]*ChannelResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, "New Name", data.Name)
	// URL masked: "https://new.example.com/webhook" → last 4 chars "hook"
	assert.Contains(t, data.Config["url"], "hook")
	assert.Contains(t, data.Config["url"], "*")
}

func TestUpdateChannel_NotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := UpdateChannelRequest{Name: "New Name"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/v1/notifications/channels/999", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteChannel_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "To Delete",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v1/notifications/channels/%d", channel.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify it's deleted
	var found domain.NotificationChannel
	result := ts.db.First(&found, channel.ID)
	assert.Error(t, result.Error)
}

func TestDeleteChannel_NotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/v1/notifications/channels/999", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteChannel_RemovesAssociatedRules(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Create channel
	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel With Rules",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	// Create a domain for the rule
	dom := domain.NormalizedDomain{DomainName: "test.com"}
	ts.db.Create(&dom)

	// Create rules associated with this channel
	rule := domain.NotificationRule{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "critical",
	}
	ts.db.Create(&rule)

	// Delete channel
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v1/notifications/channels/%d", channel.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify associated rules are also deleted
	var ruleCount int64
	ts.db.Model(&domain.NotificationRule{}).Where("channel_id = ?", channel.ID).Count(&ruleCount)
	assert.Equal(t, int64(0), ruleCount)
}

func TestTestChannel_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Create a webhook server that succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Register a factory that creates a webhook channel pointing to our test server
	ts.channelRegistry.Register("webhook", func(config *ChannelConfig) NotificationChannel {
		return NewWebhookChannelWithClient(config.GetSetting("url"), nil, server.Client())
	})

	encrypted, _ := ts.cryptoService.Encrypt(fmt.Sprintf(`{"url":"%s"}`, server.URL))
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Test Webhook",
		ConfigEncrypted: encrypted,
		Status:          "inactive",
	}
	ts.db.Create(&channel)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/notifications/channels/%d/test", channel.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "active", resp["status"])
	assert.Equal(t, "connectivity test successful", resp["message"])

	// Verify status was persisted
	var updated domain.NotificationChannel
	ts.db.First(&updated, channel.ID)
	assert.Equal(t, "active", updated.Status)
	assert.NotNil(t, updated.LastTestedAt)
}

func TestTestChannel_Failure(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Create a server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	ts.channelRegistry.Register("webhook", func(config *ChannelConfig) NotificationChannel {
		return NewWebhookChannelWithClient(config.GetSetting("url"), nil, server.Client())
	})

	encrypted, _ := ts.cryptoService.Encrypt(fmt.Sprintf(`{"url":"%s"}`, server.URL))
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Failing Webhook",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/notifications/channels/%d/test", channel.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "500")

	// Verify status was persisted
	var updated domain.NotificationChannel
	ts.db.First(&updated, channel.ID)
	assert.Equal(t, "error", updated.Status)
}

func TestTestChannel_NotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/channels/999/test", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListChannels_WithMaskedConfig(t *testing.T) {
	ts := newHandlerTestSetup(t)

	encrypted, _ := ts.cryptoService.Encrypt(`{"host":"smtp.example.com","password":"mysecretpassword123"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "email",
		Name:            "Email Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/notifications/channels", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]ChannelResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp["data"], 1)

	data := resp["data"][0]
	// "smtp.example.com" → last 4 chars ".com", rest masked
	assert.Equal(t, "************.com", data.Config["host"])
	// "mysecretpassword123" → last 4 chars "d123", rest masked
	assert.Equal(t, "***************d123", data.Config["password"])
}

// --- Rule CRUD tests ---

func TestListRules_Empty(t *testing.T) {
	ts := newHandlerTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/notifications/rules", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]RuleResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Empty(t, resp["data"])
}

func TestCreateRule_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	// Create prerequisite data
	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	body := CreateRuleRequest{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "critical",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/rules", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]*RuleResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, dom.ID, data.DomainID)
	assert.Equal(t, channel.ID, data.ChannelID)
	assert.Equal(t, "critical", data.SeverityFilter)
}

func TestCreateRule_InvalidSeverity(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	body := CreateRuleRequest{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "invalid_severity",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/rules", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateRule_DomainNotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	body := CreateRuleRequest{
		DomainID:       999,
		ChannelID:      channel.ID,
		SeverityFilter: "critical",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/rules", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateRule_ChannelNotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	body := CreateRuleRequest{
		DomainID:       dom.ID,
		ChannelID:      999,
		SeverityFilter: "critical",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/rules", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateRule_MaxRulesPerDomain(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	// Create 10 rules (the max)
	for i := 0; i < 10; i++ {
		rule := domain.NotificationRule{
			DomainID:       dom.ID,
			ChannelID:      channel.ID,
			SeverityFilter: "warning",
		}
		ts.db.Create(&rule)
	}

	// Attempt to create the 11th rule
	body := CreateRuleRequest{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "critical",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/notifications/rules", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	errorBody := resp["error"].(map[string]interface{})
	assert.Contains(t, errorBody["message"], "10")
}

func TestCreateRule_AllValidSeverities(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "severity-test.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	validSeverities := []string{"informational", "warning", "critical", "expired"}
	for _, severity := range validSeverities {
		t.Run(severity, func(t *testing.T) {
			body := CreateRuleRequest{
				DomainID:       dom.ID,
				ChannelID:      channel.ID,
				SeverityFilter: severity,
			}
			jsonBody, _ := json.Marshal(body)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/v1/notifications/rules", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			ts.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
		})
	}
}

func TestUpdateRule_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel1 := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel 1",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel1)

	channel2 := domain.NotificationChannel{
		ChannelType:     "email",
		Name:            "Channel 2",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel2)

	rule := domain.NotificationRule{
		DomainID:       dom.ID,
		ChannelID:      channel1.ID,
		SeverityFilter: "warning",
	}
	ts.db.Create(&rule)

	body := UpdateRuleRequest{
		ChannelID:      channel2.ID,
		SeverityFilter: "critical",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v1/notifications/rules/%d", rule.ID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]*RuleResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"]
	assert.Equal(t, channel2.ID, data.ChannelID)
	assert.Equal(t, "critical", data.SeverityFilter)
}

func TestUpdateRule_NotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	body := UpdateRuleRequest{SeverityFilter: "critical"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/v1/notifications/rules/999", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateRule_InvalidSeverity(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	rule := domain.NotificationRule{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "warning",
	}
	ts.db.Create(&rule)

	body := UpdateRuleRequest{SeverityFilter: "invalid"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v1/notifications/rules/%d", rule.ID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateRule_ChannelNotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	rule := domain.NotificationRule{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "warning",
	}
	ts.db.Create(&rule)

	body := UpdateRuleRequest{ChannelID: 999}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/v1/notifications/rules/%d", rule.ID), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteRule_Success(t *testing.T) {
	ts := newHandlerTestSetup(t)

	dom := domain.NormalizedDomain{DomainName: "example.com"}
	ts.db.Create(&dom)

	encrypted, _ := ts.cryptoService.Encrypt(`{"url":"https://example.com"}`)
	channel := domain.NotificationChannel{
		ChannelType:     "webhook",
		Name:            "Channel",
		ConfigEncrypted: encrypted,
		Status:          "active",
	}
	ts.db.Create(&channel)

	rule := domain.NotificationRule{
		DomainID:       dom.ID,
		ChannelID:      channel.ID,
		SeverityFilter: "critical",
	}
	ts.db.Create(&rule)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/v1/notifications/rules/%d", rule.ID), nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify it's deleted
	var found domain.NotificationRule
	result := ts.db.First(&found, rule.ID)
	assert.Error(t, result.Error)
}

func TestDeleteRule_NotFound(t *testing.T) {
	ts := newHandlerTestSetup(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/v1/notifications/rules/999", nil)
	ts.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestInvalidChannelIDParameter(t *testing.T) {
	ts := newHandlerTestSetup(t)

	tests := []struct {
		method string
		path   string
	}{
		{"PUT", "/api/v1/notifications/channels/invalid"},
		{"DELETE", "/api/v1/notifications/channels/invalid"},
		{"POST", "/api/v1/notifications/channels/invalid/test"},
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

func TestInvalidRuleIDParameter(t *testing.T) {
	ts := newHandlerTestSetup(t)

	tests := []struct {
		method string
		path   string
	}{
		{"PUT", "/api/v1/notifications/rules/invalid"},
		{"DELETE", "/api/v1/notifications/rules/invalid"},
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

// --- mockNotificationChannel for registry tests ---

type mockNotificationChannel struct {
	channelType string
	testErr     error
}

func (m *mockNotificationChannel) Send(_ context.Context, _ *Notification) error {
	return nil
}

func (m *mockNotificationChannel) TestConnection(_ context.Context, _ *ChannelConfig) error {
	return m.testErr
}

func (m *mockNotificationChannel) ChannelType() string {
	return m.channelType
}

func TestChannelRegistry_RegisterAndGet(t *testing.T) {
	registry := NewChannelRegistry()

	registry.Register("test", func(config *ChannelConfig) NotificationChannel {
		return &mockNotificationChannel{channelType: "test"}
	})

	factory, err := registry.Get("test")
	require.NoError(t, err)
	assert.NotNil(t, factory)

	ch := factory(&ChannelConfig{Settings: map[string]string{}})
	assert.Equal(t, "test", ch.ChannelType())
}

func TestChannelRegistry_GetUnsupported(t *testing.T) {
	registry := NewChannelRegistry()

	_, err := registry.Get("unsupported")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported channel type")
}

func TestIsValidSeverity(t *testing.T) {
	assert.True(t, isValidSeverity("informational"))
	assert.True(t, isValidSeverity("warning"))
	assert.True(t, isValidSeverity("critical"))
	assert.True(t, isValidSeverity("expired"))
	assert.False(t, isValidSeverity("unknown"))
	assert.False(t, isValidSeverity(""))
	assert.False(t, isValidSeverity("CRITICAL"))
}
