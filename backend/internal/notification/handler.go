package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"domainradar/internal/audit"
	"domainradar/internal/auth"
	"domainradar/internal/crypto"
	"domainradar/internal/domain"
	"domainradar/internal/errors"
)

const (
	// testChannelTimeout is the timeout for channel connectivity tests (10 seconds per requirement 5.8).
	testChannelTimeout = 10 * time.Second

	// maxRulesPerDomain is the maximum number of notification rules per domain (requirement 5.5).
	maxRulesPerDomain = 10
)

// ChannelRegistry maps channel type names to their NotificationChannel implementations.
// It allows creating channel instances for connectivity testing.
type ChannelRegistry struct {
	factories map[string]ChannelFactory
}

// ChannelFactory is a function that creates a NotificationChannel from a ChannelConfig.
type ChannelFactory func(config *ChannelConfig) NotificationChannel

// NewChannelRegistry creates a new ChannelRegistry.
func NewChannelRegistry() *ChannelRegistry {
	return &ChannelRegistry{
		factories: make(map[string]ChannelFactory),
	}
}

// Register adds a channel factory for the given channel type.
func (r *ChannelRegistry) Register(channelType string, factory ChannelFactory) {
	r.factories[channelType] = factory
}

// Get returns the factory for the given channel type, or an error if unsupported.
func (r *ChannelRegistry) Get(channelType string) (ChannelFactory, error) {
	factory, ok := r.factories[channelType]
	if !ok {
		return nil, fmt.Errorf("unsupported channel type: %s", channelType)
	}
	return factory, nil
}

// NotificationHandler handles notification channel and rule CRUD operations.
type NotificationHandler struct {
	db              *gorm.DB
	cryptoService   *crypto.CryptoService
	channelRegistry *ChannelRegistry
	auditService    *audit.Service
	logger          *zap.Logger
}

// NewNotificationHandler creates a new NotificationHandler.
func NewNotificationHandler(
	db *gorm.DB,
	cryptoService *crypto.CryptoService,
	channelRegistry *ChannelRegistry,
	auditService *audit.Service,
	logger *zap.Logger,
) *NotificationHandler {
	return &NotificationHandler{
		db:              db,
		cryptoService:   cryptoService,
		channelRegistry: channelRegistry,
		auditService:    auditService,
		logger:          logger,
	}
}

// RegisterRoutes registers notification channel and rule endpoints on the given Gin router group.
// All endpoints require the ConfigureIntegrations permission.
func (h *NotificationHandler) RegisterRoutes(rg *gin.RouterGroup) {
	notifications := rg.Group("/notifications")
	notifications.Use(auth.RequirePermission(auth.ConfigureIntegrations))
	{
		// Channel endpoints
		channels := notifications.Group("/channels")
		{
			channels.GET("", h.ListChannels)
			channels.POST("", h.CreateChannel)
			channels.PUT("/:id", h.UpdateChannel)
			channels.DELETE("/:id", h.DeleteChannel)
			channels.POST("/:id/test", h.TestChannel)
		}

		// Rule endpoints
		rules := notifications.Group("/rules")
		{
			rules.GET("", h.ListRules)
			rules.POST("", h.CreateRule)
			rules.PUT("/:id", h.UpdateRule)
			rules.DELETE("/:id", h.DeleteRule)
		}
	}
}

// --- Request/Response types ---

// CreateChannelRequest is the request body for creating a notification channel.
type CreateChannelRequest struct {
	ChannelType string            `json:"channel_type" binding:"required"`
	Name        string            `json:"name" binding:"required"`
	Config      map[string]string `json:"config" binding:"required"`
}

// UpdateChannelRequest is the request body for updating a notification channel.
type UpdateChannelRequest struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config,omitempty"`
}

// ChannelResponse is the API response for a notification channel.
type ChannelResponse struct {
	ID           uint              `json:"id"`
	ChannelType  string            `json:"channel_type"`
	Name         string            `json:"name"`
	Config       map[string]string `json:"config"`
	Status       string            `json:"status"`
	LastTestedAt *time.Time        `json:"last_tested_at"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// CreateRuleRequest is the request body for creating a notification rule.
type CreateRuleRequest struct {
	DomainID       uint   `json:"domain_id" binding:"required"`
	ChannelID      uint   `json:"channel_id" binding:"required"`
	SeverityFilter string `json:"severity_filter" binding:"required"`
}

// UpdateRuleRequest is the request body for updating a notification rule.
type UpdateRuleRequest struct {
	ChannelID      uint   `json:"channel_id"`
	SeverityFilter string `json:"severity_filter"`
}

// RuleResponse is the API response for a notification rule.
type RuleResponse struct {
	ID             uint      `json:"id"`
	DomainID       uint      `json:"domain_id"`
	ChannelID      uint      `json:"channel_id"`
	SeverityFilter string    `json:"severity_filter"`
	CreatedAt      time.Time `json:"created_at"`
}

// --- Channel Handlers ---

// ListChannels returns all configured notification channels with masked config values.
func (h *NotificationHandler) ListChannels(c *gin.Context) {
	var channels []domain.NotificationChannel
	if err := h.db.Find(&channels).Error; err != nil {
		h.logger.Error("failed to list notification channels", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to list notification channels"))
		return
	}

	responses := make([]ChannelResponse, 0, len(channels))
	for _, ch := range channels {
		resp, err := h.buildChannelResponse(&ch)
		if err != nil {
			h.logger.Error("failed to build channel response", zap.Uint("channel_id", ch.ID), zap.Error(err))
			continue
		}
		responses = append(responses, *resp)
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

// CreateChannel creates a new notification channel with encrypted config.
func (h *NotificationHandler) CreateChannel(c *gin.Context) {
	var req CreateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	// Validate channel type is supported
	if _, err := h.channelRegistry.Get(req.ChannelType); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("unsupported channel type: "+req.ChannelType))
		return
	}

	// Validate config is non-empty
	if len(req.Config) == 0 {
		errors.ErrorResponse(c, errors.BadRequest("config is required"))
		return
	}

	// Encrypt config
	encryptedConfig, err := h.encryptConfig(req.Config)
	if err != nil {
		h.logger.Error("failed to encrypt channel config", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to encrypt channel configuration"))
		return
	}

	channel := domain.NotificationChannel{
		ChannelType:     req.ChannelType,
		Name:            req.Name,
		ConfigEncrypted: encryptedConfig,
		Status:          "inactive",
	}

	if err := h.db.Create(&channel).Error; err != nil {
		h.logger.Error("failed to create notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create notification channel"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "CREATE", "notification_channel", strconv.Itoa(int(channel.ID)), map[string]interface{}{
		"channel_type": req.ChannelType,
		"name":         req.Name,
		"config":       "******",
	})

	resp, err := h.buildChannelResponse(&channel)
	if err != nil {
		h.logger.Error("failed to build channel response", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to build response"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": resp})
}

// UpdateChannel updates an existing notification channel.
func (h *NotificationHandler) UpdateChannel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid channel ID"))
		return
	}

	var channel domain.NotificationChannel
	if err := h.db.First(&channel, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("notification channel not found"))
			return
		}
		h.logger.Error("failed to query notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query notification channel"))
		return
	}

	var req UpdateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	changes := make(map[string]interface{})

	if req.Name != "" {
		channel.Name = req.Name
		changes["name"] = req.Name
	}

	if req.Config != nil {
		encryptedConfig, err := h.encryptConfig(req.Config)
		if err != nil {
			h.logger.Error("failed to encrypt channel config", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to encrypt channel configuration"))
			return
		}
		channel.ConfigEncrypted = encryptedConfig
		changes["config"] = "******"
	}

	if err := h.db.Save(&channel).Error; err != nil {
		h.logger.Error("failed to update notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to update notification channel"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "UPDATE", "notification_channel", strconv.Itoa(int(channel.ID)), changes)

	resp, err := h.buildChannelResponse(&channel)
	if err != nil {
		h.logger.Error("failed to build channel response", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to build response"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// DeleteChannel deletes a notification channel.
func (h *NotificationHandler) DeleteChannel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid channel ID"))
		return
	}

	var channel domain.NotificationChannel
	if err := h.db.First(&channel, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("notification channel not found"))
			return
		}
		h.logger.Error("failed to query notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query notification channel"))
		return
	}

	// Delete associated rules first
	if err := h.db.Where("channel_id = ?", channel.ID).Delete(&domain.NotificationRule{}).Error; err != nil {
		h.logger.Error("failed to delete associated notification rules", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete associated notification rules"))
		return
	}

	if err := h.db.Delete(&channel).Error; err != nil {
		h.logger.Error("failed to delete notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete notification channel"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "DELETE", "notification_channel", strconv.Itoa(int(channel.ID)), map[string]interface{}{
		"channel_type": channel.ChannelType,
		"name":         channel.Name,
	})

	c.JSON(http.StatusOK, gin.H{"message": "notification channel deleted"})
}

// TestChannel tests the connectivity of a notification channel with a 10-second timeout.
func (h *NotificationHandler) TestChannel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid channel ID"))
		return
	}

	var channel domain.NotificationChannel
	if err := h.db.First(&channel, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("notification channel not found"))
			return
		}
		h.logger.Error("failed to query notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query notification channel"))
		return
	}

	// Get the channel factory
	factory, err := h.channelRegistry.Get(channel.ChannelType)
	if err != nil {
		errors.ErrorResponse(c, errors.InternalServer("no implementation found for channel type: "+channel.ChannelType))
		return
	}

	// Decrypt config
	config, err := h.decryptConfig(channel.ConfigEncrypted)
	if err != nil {
		h.logger.Error("failed to decrypt channel config", zap.Uint("channel_id", channel.ID), zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to decrypt channel configuration"))
		return
	}

	// Create channel instance and test connectivity with 10-second timeout
	channelInstance := factory(config)
	ctx, cancel := context.WithTimeout(c.Request.Context(), testChannelTimeout)
	defer cancel()

	testErr := channelInstance.TestConnection(ctx, config)
	now := time.Now()

	if testErr != nil {
		channel.Status = "error"
		channel.LastTestedAt = &now
		h.db.Model(&channel).Updates(map[string]interface{}{
			"status":         "error",
			"last_tested_at": now,
		})
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": testErr.Error(),
		})
		return
	}

	channel.Status = "active"
	channel.LastTestedAt = &now
	h.db.Model(&channel).Updates(map[string]interface{}{
		"status":         "active",
		"last_tested_at": now,
	})
	c.JSON(http.StatusOK, gin.H{
		"status":  "active",
		"message": "connectivity test successful",
	})
}

// --- Rule Handlers ---

// ListRules returns all notification rules.
func (h *NotificationHandler) ListRules(c *gin.Context) {
	var rules []domain.NotificationRule
	if err := h.db.Find(&rules).Error; err != nil {
		h.logger.Error("failed to list notification rules", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to list notification rules"))
		return
	}

	responses := make([]RuleResponse, 0, len(rules))
	for _, rule := range rules {
		responses = append(responses, RuleResponse{
			ID:             rule.ID,
			DomainID:       rule.DomainID,
			ChannelID:      rule.ChannelID,
			SeverityFilter: rule.SeverityFilter,
			CreatedAt:      rule.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

// CreateRule creates a new notification rule, enforcing max 10 rules per domain.
func (h *NotificationHandler) CreateRule(c *gin.Context) {
	var req CreateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	// Validate severity filter
	if !isValidSeverity(req.SeverityFilter) {
		errors.ErrorResponse(c, errors.BadRequest("invalid severity_filter; must be one of: informational, warning, critical, expired"))
		return
	}

	// Validate domain exists
	var domainRecord domain.NormalizedDomain
	if err := h.db.First(&domainRecord, req.DomainID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("domain not found"))
			return
		}
		h.logger.Error("failed to query domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query domain"))
		return
	}

	// Validate channel exists
	var channel domain.NotificationChannel
	if err := h.db.First(&channel, req.ChannelID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("notification channel not found"))
			return
		}
		h.logger.Error("failed to query notification channel", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query notification channel"))
		return
	}

	// Enforce max 10 rules per domain
	var count int64
	h.db.Model(&domain.NotificationRule{}).Where("domain_id = ?", req.DomainID).Count(&count)
	if count >= maxRulesPerDomain {
		errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("maximum of %d notification rules per domain reached", maxRulesPerDomain)))
		return
	}

	rule := domain.NotificationRule{
		DomainID:       req.DomainID,
		ChannelID:      req.ChannelID,
		SeverityFilter: req.SeverityFilter,
	}

	if err := h.db.Create(&rule).Error; err != nil {
		h.logger.Error("failed to create notification rule", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create notification rule"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "CREATE", "notification_rule", strconv.Itoa(int(rule.ID)), map[string]interface{}{
		"domain_id":       req.DomainID,
		"channel_id":      req.ChannelID,
		"severity_filter": req.SeverityFilter,
	})

	c.JSON(http.StatusCreated, gin.H{"data": RuleResponse{
		ID:             rule.ID,
		DomainID:       rule.DomainID,
		ChannelID:      rule.ChannelID,
		SeverityFilter: rule.SeverityFilter,
		CreatedAt:      rule.CreatedAt,
	}})
}

// UpdateRule updates an existing notification rule.
func (h *NotificationHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule ID"))
		return
	}

	var rule domain.NotificationRule
	if err := h.db.First(&rule, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("notification rule not found"))
			return
		}
		h.logger.Error("failed to query notification rule", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query notification rule"))
		return
	}

	var req UpdateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	changes := make(map[string]interface{})

	if req.ChannelID != 0 {
		// Validate channel exists
		var channel domain.NotificationChannel
		if err := h.db.First(&channel, req.ChannelID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				errors.ErrorResponse(c, errors.NotFound("notification channel not found"))
				return
			}
			h.logger.Error("failed to query notification channel", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to query notification channel"))
			return
		}
		rule.ChannelID = req.ChannelID
		changes["channel_id"] = req.ChannelID
	}

	if req.SeverityFilter != "" {
		if !isValidSeverity(req.SeverityFilter) {
			errors.ErrorResponse(c, errors.BadRequest("invalid severity_filter; must be one of: informational, warning, critical, expired"))
			return
		}
		rule.SeverityFilter = req.SeverityFilter
		changes["severity_filter"] = req.SeverityFilter
	}

	if err := h.db.Save(&rule).Error; err != nil {
		h.logger.Error("failed to update notification rule", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to update notification rule"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "UPDATE", "notification_rule", strconv.Itoa(int(rule.ID)), changes)

	c.JSON(http.StatusOK, gin.H{"data": RuleResponse{
		ID:             rule.ID,
		DomainID:       rule.DomainID,
		ChannelID:      rule.ChannelID,
		SeverityFilter: rule.SeverityFilter,
		CreatedAt:      rule.CreatedAt,
	}})
}

// DeleteRule deletes a notification rule.
func (h *NotificationHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule ID"))
		return
	}

	var rule domain.NotificationRule
	if err := h.db.First(&rule, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("notification rule not found"))
			return
		}
		h.logger.Error("failed to query notification rule", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query notification rule"))
		return
	}

	if err := h.db.Delete(&rule).Error; err != nil {
		h.logger.Error("failed to delete notification rule", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete notification rule"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "DELETE", "notification_rule", strconv.Itoa(int(rule.ID)), map[string]interface{}{
		"domain_id":       rule.DomainID,
		"channel_id":      rule.ChannelID,
		"severity_filter": rule.SeverityFilter,
	})

	c.JSON(http.StatusOK, gin.H{"message": "notification rule deleted"})
}

// --- Helper methods ---

// encryptConfig marshals the config map to JSON and encrypts it.
func (h *NotificationHandler) encryptConfig(config map[string]string) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}
	return h.cryptoService.Encrypt(string(data))
}

// decryptConfig decrypts and unmarshals the encrypted config into a ChannelConfig.
func (h *NotificationHandler) decryptConfig(encrypted string) (*ChannelConfig, error) {
	if encrypted == "" {
		return &ChannelConfig{Settings: map[string]string{}}, nil
	}

	plaintext, err := h.cryptoService.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt config: %w", err)
	}

	var settings map[string]string
	if err := json.Unmarshal([]byte(plaintext), &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &ChannelConfig{Settings: settings}, nil
}

// maskConfig decrypts config and returns it with values masked (last 4 chars visible).
func (h *NotificationHandler) maskConfig(encrypted string) (map[string]string, error) {
	if encrypted == "" {
		return map[string]string{}, nil
	}

	plaintext, err := h.cryptoService.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt config for masking: %w", err)
	}

	var settings map[string]string
	if err := json.Unmarshal([]byte(plaintext), &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	masked := make(map[string]string, len(settings))
	for k, v := range settings {
		masked[k] = crypto.MaskCredential(v)
	}
	return masked, nil
}

// buildChannelResponse builds the API response for a notification channel, masking config values.
func (h *NotificationHandler) buildChannelResponse(channel *domain.NotificationChannel) (*ChannelResponse, error) {
	maskedConfig, err := h.maskConfig(channel.ConfigEncrypted)
	if err != nil {
		return nil, err
	}

	return &ChannelResponse{
		ID:           channel.ID,
		ChannelType:  channel.ChannelType,
		Name:         channel.Name,
		Config:       maskedConfig,
		Status:       channel.Status,
		LastTestedAt: channel.LastTestedAt,
		CreatedAt:    channel.CreatedAt,
		UpdatedAt:    channel.UpdatedAt,
	}, nil
}

// getUserID extracts the authenticated user's ID from the Gin context.
func (h *NotificationHandler) getUserID(c *gin.Context) uint {
	userVal, exists := c.Get("user")
	if !exists {
		return 0
	}
	user, ok := userVal.(*auth.UserInfo)
	if !ok {
		return 0
	}
	return user.ID
}

// isValidSeverity checks whether a severity filter string is valid.
func isValidSeverity(severity string) bool {
	switch severity {
	case "informational", "warning", "critical", "expired":
		return true
	default:
		return false
	}
}
