package adapter

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
	// maxAccountsPerType is the maximum number of accounts allowed per registrar type.
	maxAccountsPerType = 20

	// maxCredentialLength is the maximum allowed length for credential values.
	maxCredentialLength = 512

	// testConnectionTimeout is the timeout for connectivity tests.
	testConnectionTimeout = 30 * time.Second

	// maxRecentErrors is the number of recent error entries returned in the status endpoint.
	maxRecentErrors = 50
)

// SyncTrigger is an interface for triggering sync cycles. This decouples the handler
// from the sync package to avoid circular imports.
type SyncTrigger interface {
	RunSyncCycle(ctx context.Context, accountID uint) error
}

// RegistrarHandler handles registrar configuration management CRUD operations.
type RegistrarHandler struct {
	db              *gorm.DB
	cryptoService   *crypto.CryptoService
	adapterRegistry *AdapterRegistry
	auditService    *audit.Service
	syncTrigger     SyncTrigger
	logger          *zap.Logger
}

// NewRegistrarHandler creates a new RegistrarHandler.
func NewRegistrarHandler(
	db *gorm.DB,
	cryptoService *crypto.CryptoService,
	adapterRegistry *AdapterRegistry,
	auditService *audit.Service,
	logger *zap.Logger,
	opts ...RegistrarHandlerOption,
) *RegistrarHandler {
	h := &RegistrarHandler{
		db:              db,
		cryptoService:   cryptoService,
		adapterRegistry: adapterRegistry,
		auditService:    auditService,
		logger:          logger,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegistrarHandlerOption is a functional option for RegistrarHandler.
type RegistrarHandlerOption func(*RegistrarHandler)

// WithSyncTrigger sets the SyncTrigger for manual sync operations.
func WithSyncTrigger(st SyncTrigger) RegistrarHandlerOption {
	return func(h *RegistrarHandler) {
		h.syncTrigger = st
	}
}

// RegisterRoutes registers registrar endpoints on the given Gin router group.
// All endpoints require the ConfigureIntegrations permission.
func (h *RegistrarHandler) RegisterRoutes(rg *gin.RouterGroup) {
	registrars := rg.Group("/registrars")
	registrars.Use(auth.RequirePermission(auth.ConfigureIntegrations))
	{
		registrars.GET("", h.ListRegistrars)
		registrars.POST("", h.CreateRegistrar)
		registrars.PUT("/:id", h.UpdateRegistrar)
		registrars.DELETE("/:id", h.DeleteRegistrar)
		registrars.POST("/:id/test", h.TestConnection)
		registrars.POST("/:id/sync", h.TriggerSync)
		registrars.POST("/:id/preview-sync", h.PreviewSync)
		registrars.POST("/:id/import", h.SelectiveImport)
		registrars.GET("/:id/status", h.GetStatus)
	}
}

// --- Request/Response types ---

// CreateRegistrarRequest is the request body for creating a registrar account.
type CreateRegistrarRequest struct {
	RegistrarType string            `json:"registrar_type" binding:"required"`
	DisplayName   string            `json:"display_name" binding:"required"`
	AccountName   string            `json:"account_name" binding:"required"`
	Credentials   map[string]string `json:"credentials" binding:"required"`
}

// UpdateRegistrarRequest is the request body for updating a registrar account.
type UpdateRegistrarRequest struct {
	AccountName string            `json:"account_name"`
	Credentials map[string]string `json:"credentials,omitempty"`
}

// RegistrarAccountResponse is the API response for a registrar account.
type RegistrarAccountResponse struct {
	ID                uint              `json:"id"`
	RegistrarConfigID uint              `json:"registrar_config_id"`
	RegistrarType     string            `json:"registrar_type"`
	DisplayName       string            `json:"display_name"`
	AccountName       string            `json:"account_name"`
	Credentials       map[string]string `json:"credentials"`
	Status            string            `json:"status"`
	SyncIntervalHours int               `json:"sync_interval_hours"`
	LastSyncAt        *time.Time        `json:"last_sync_at"`
	DomainCount       int               `json:"domain_count"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// RegistrarStatusResponse is the API response for the status endpoint.
type RegistrarStatusResponse struct {
	ID            uint              `json:"id"`
	AccountName   string            `json:"account_name"`
	RegistrarType string            `json:"registrar_type"`
	Status        string            `json:"status"`
	LastSyncAt    *time.Time        `json:"last_sync_at"`
	DomainCount   int               `json:"domain_count"`
	RecentErrors  []SyncLogResponse `json:"recent_errors"`
}

// SyncLogResponse is the API response for a sync log entry.
type SyncLogResponse struct {
	ID             uint       `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
	DomainsSynced  int        `json:"domains_synced"`
	DomainsUpdated int        `json:"domains_updated"`
	Status         string     `json:"status"`
	ErrorMessage   string     `json:"error_message"`
}

// --- Handlers ---

// ListRegistrars returns all configured registrar accounts grouped by config.
func (h *RegistrarHandler) ListRegistrars(c *gin.Context) {
	var accounts []domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").Find(&accounts).Error; err != nil {
		h.logger.Error("failed to list registrar accounts", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to list registrar accounts"))
		return
	}

	responses := make([]RegistrarAccountResponse, 0, len(accounts))
	for _, acct := range accounts {
		resp, err := h.buildAccountResponse(&acct)
		if err != nil {
			h.logger.Error("failed to build account response", zap.Uint("account_id", acct.ID), zap.Error(err))
			continue
		}
		responses = append(responses, *resp)
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

// CreateRegistrar creates a new registrar configuration and account.
func (h *RegistrarHandler) CreateRegistrar(c *gin.Context) {
	var req CreateRegistrarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	// Validate required fields non-empty
	if err := h.validateCreateRequest(&req); err != nil {
		errors.ErrorResponse(c, err)
		return
	}

	// Validate credentials length
	if err := h.validateCredentials(req.Credentials); err != nil {
		errors.ErrorResponse(c, err)
		return
	}

	// Validate registrar type is supported
	if _, err := h.adapterRegistry.Get(req.RegistrarType); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("unsupported registrar type: "+req.RegistrarType))
		return
	}

	// Enforce max 20 accounts per registrar type
	if err := h.validateAccountLimit(req.RegistrarType); err != nil {
		errors.ErrorResponse(c, err)
		return
	}

	// Find or create the registrar config
	var config domain.RegistrarConfig
	result := h.db.Where("registrar_type = ?", req.RegistrarType).First(&config)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			config = domain.RegistrarConfig{
				RegistrarType: req.RegistrarType,
				DisplayName:   req.DisplayName,
			}
			if err := h.db.Create(&config).Error; err != nil {
				h.logger.Error("failed to create registrar config", zap.Error(err))
				errors.ErrorResponse(c, errors.InternalServer("failed to create registrar configuration"))
				return
			}
		} else {
			h.logger.Error("failed to query registrar config", zap.Error(result.Error))
			errors.ErrorResponse(c, errors.InternalServer("failed to query registrar configuration"))
			return
		}
	}

	// Encrypt credentials
	encryptedCreds, err := h.encryptCredentials(req.Credentials)
	if err != nil {
		h.logger.Error("failed to encrypt credentials", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to encrypt credentials"))
		return
	}

	// Create the account
	account := domain.RegistrarAccount{
		RegistrarConfigID:    config.ID,
		AccountName:          req.AccountName,
		CredentialsEncrypted: encryptedCreds,
		Status:               "disconnected",
	}
	if err := h.db.Create(&account).Error; err != nil {
		h.logger.Error("failed to create registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create registrar account"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "CREATE", "registrar_account", strconv.Itoa(int(account.ID)), map[string]interface{}{
		"registrar_type": req.RegistrarType,
		"account_name":   req.AccountName,
		"credentials":    "******",
	})

	// Reload with preloaded config for response
	h.db.Preload("RegistrarConfig").First(&account, account.ID)

	resp, err := h.buildAccountResponse(&account)
	if err != nil {
		h.logger.Error("failed to build account response", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to build response"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": resp})
}

// UpdateRegistrar updates an existing registrar account.
func (h *RegistrarHandler) UpdateRegistrar(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	var req UpdateRegistrarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	changes := make(map[string]interface{})

	if req.AccountName != "" {
		account.AccountName = req.AccountName
		changes["account_name"] = req.AccountName
	}

	if req.Credentials != nil {
		// Validate credentials length
		if err := h.validateCredentials(req.Credentials); err != nil {
			errors.ErrorResponse(c, err)
			return
		}

		// Re-encrypt credentials
		encryptedCreds, err := h.encryptCredentials(req.Credentials)
		if err != nil {
			h.logger.Error("failed to encrypt credentials", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to encrypt credentials"))
			return
		}
		account.CredentialsEncrypted = encryptedCreds
		changes["credentials"] = "******"
	}

	if err := h.db.Save(&account).Error; err != nil {
		h.logger.Error("failed to update registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to update registrar account"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "UPDATE", "registrar_account", strconv.Itoa(int(account.ID)), changes)

	resp, err := h.buildAccountResponse(&account)
	if err != nil {
		h.logger.Error("failed to build account response", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to build response"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// DeleteRegistrar deletes a registrar account.
func (h *RegistrarHandler) DeleteRegistrar(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	if err := h.db.Delete(&account).Error; err != nil {
		h.logger.Error("failed to delete registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete registrar account"))
		return
	}

	// Record audit log
	userID := h.getUserID(c)
	h.auditService.RecordAction(userID, "DELETE", "registrar_account", strconv.Itoa(int(account.ID)), map[string]interface{}{
		"account_name": account.AccountName,
	})

	c.JSON(http.StatusOK, gin.H{"message": "registrar account deleted"})
}

// TestConnection tests the connectivity of a registrar account.
func (h *RegistrarHandler) TestConnection(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	// Get the adapter for this registrar type
	adapter, err := h.adapterRegistry.Get(account.RegistrarConfig.RegistrarType)
	if err != nil {
		errors.ErrorResponse(c, errors.InternalServer("no adapter found for registrar type: "+account.RegistrarConfig.RegistrarType))
		return
	}

	// Decrypt credentials
	cred, err := h.decryptCredentials(account.CredentialsEncrypted)
	if err != nil {
		h.logger.Error("failed to decrypt credentials", zap.Uint("account_id", account.ID), zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to decrypt credentials"))
		return
	}

	// Execute connectivity test with 30-second timeout
	ctx, cancel := context.WithTimeout(c.Request.Context(), testConnectionTimeout)
	defer cancel()

	testErr := adapter.TestConnection(ctx, cred)

	// Persist connection status
	if testErr != nil {
		account.Status = "disconnected"
		h.db.Model(&account).Update("status", "disconnected")
		c.JSON(http.StatusOK, gin.H{
			"status":  "disconnected",
			"message": testErr.Error(),
		})
		return
	}

	account.Status = "connected"
	h.db.Model(&account).Update("status", "connected")
	c.JSON(http.StatusOK, gin.H{
		"status":  "connected",
		"message": "connectivity test successful",
	})
}

// TriggerSync triggers an immediate sync for a registrar account.
// It validates the account exists and is in "connected" status, then launches
// RunSyncCycle in a goroutine and returns 202 Accepted immediately.
func (h *RegistrarHandler) TriggerSync(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Uint("account_id", uint(id)), zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	if account.Status != "connected" {
		errors.ErrorResponse(c, errors.BadRequest("cannot sync account in '"+account.Status+"' status; account must be connected"))
		return
	}

	if h.syncTrigger == nil {
		errors.ErrorResponse(c, errors.InternalServer("sync service not available"))
		return
	}

	// Launch sync in a background goroutine and return immediately.
	go func(accountID uint) {
		if syncErr := h.syncTrigger.RunSyncCycle(context.Background(), accountID); syncErr != nil {
			h.logger.Error("manual sync cycle failed",
				zap.Uint("account_id", accountID),
				zap.Error(syncErr),
			)
		}
	}(account.ID)

	c.JSON(http.StatusAccepted, gin.H{
		"message":    "sync triggered",
		"account_id": account.ID,
	})
}

// GetStatus returns the sync status, last sync time, domain count, and recent errors.
func (h *RegistrarHandler) GetStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	// Get last 50 error sync logs
	var syncLogs []domain.SyncLog
	h.db.Where("registrar_account_id = ? AND status IN ?", account.ID, []string{"failed", "timeout"}).
		Order("started_at DESC").
		Limit(maxRecentErrors).
		Find(&syncLogs)

	recentErrors := make([]SyncLogResponse, 0, len(syncLogs))
	for _, log := range syncLogs {
		recentErrors = append(recentErrors, SyncLogResponse{
			ID:             log.ID,
			StartedAt:      log.StartedAt,
			EndedAt:        log.EndedAt,
			DomainsSynced:  log.DomainsSynced,
			DomainsUpdated: log.DomainsUpdated,
			Status:         log.Status,
			ErrorMessage:   log.ErrorMessage,
		})
	}

	resp := RegistrarStatusResponse{
		ID:            account.ID,
		AccountName:   account.AccountName,
		RegistrarType: account.RegistrarConfig.RegistrarType,
		Status:        account.Status,
		LastSyncAt:    account.LastSyncAt,
		DomainCount:   account.DomainCount,
		RecentErrors:  recentErrors,
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// --- Helper methods ---

// validateCreateRequest validates that all required fields in a create request are non-empty.
func (h *RegistrarHandler) validateCreateRequest(req *CreateRegistrarRequest) *errors.AppError {
	if req.RegistrarType == "" {
		return errors.BadRequest("registrar_type is required")
	}
	if req.DisplayName == "" {
		return errors.BadRequest("display_name is required")
	}
	if req.AccountName == "" {
		return errors.BadRequest("account_name is required")
	}
	if len(req.Credentials) == 0 {
		return errors.BadRequest("credentials are required")
	}
	// Validate credential keys have non-empty values
	for key, val := range req.Credentials {
		if val == "" {
			return errors.BadRequest(fmt.Sprintf("credential field '%s' must not be empty", key))
		}
	}
	return nil
}

// validateCredentials checks that no credential value exceeds 512 characters.
func (h *RegistrarHandler) validateCredentials(creds map[string]string) *errors.AppError {
	for key, val := range creds {
		if len(val) > maxCredentialLength {
			return errors.BadRequest(fmt.Sprintf("credential field '%s' exceeds maximum length of %d characters", key, maxCredentialLength))
		}
	}
	return nil
}

// validateAccountLimit checks that the maximum number of accounts per registrar type is not exceeded.
func (h *RegistrarHandler) validateAccountLimit(registrarType string) *errors.AppError {
	var count int64
	h.db.Model(&domain.RegistrarAccount{}).
		Joins("JOIN registrar_configs ON registrar_configs.id = registrar_accounts.registrar_config_id").
		Where("registrar_configs.registrar_type = ?", registrarType).
		Count(&count)

	if count >= maxAccountsPerType {
		return errors.BadRequest(fmt.Sprintf("maximum of %d accounts per registrar type reached", maxAccountsPerType))
	}
	return nil
}

// encryptCredentials marshals the credentials map to JSON and encrypts it.
func (h *RegistrarHandler) encryptCredentials(creds map[string]string) (string, error) {
	data, err := json.Marshal(creds)
	if err != nil {
		return "", fmt.Errorf("failed to marshal credentials: %w", err)
	}
	return h.cryptoService.Encrypt(string(data))
}

// decryptCredentials decrypts and unmarshals credentials into a RegistrarCredential.
func (h *RegistrarHandler) decryptCredentials(encrypted string) (*RegistrarCredential, error) {
	plaintext, err := h.cryptoService.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}

	var credsMap map[string]string
	if err := json.Unmarshal([]byte(plaintext), &credsMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	cred := &RegistrarCredential{
		APIKey:         credsMap["api_key"],
		APISecret:      credsMap["api_secret"],
		Token:          credsMap["token"],
		Username:       credsMap["username"],
		AccessKeyID:    credsMap["access_key_id"],
		SecretAccessKey: credsMap["secret_access_key"],
		IPWhitelist:    credsMap["ip_whitelist"],
	}

	// Put any extra fields into the Extra map
	knownFields := map[string]bool{
		"api_key": true, "api_secret": true, "token": true,
		"username": true, "access_key_id": true, "secret_access_key": true,
		"ip_whitelist": true,
	}
	for k, v := range credsMap {
		if !knownFields[k] {
			if cred.Extra == nil {
				cred.Extra = make(map[string]string)
			}
			cred.Extra[k] = v
		}
	}

	return cred, nil
}

// maskCredentials decrypts credentials and returns them with values masked (last 4 chars visible).
func (h *RegistrarHandler) maskCredentials(encrypted string) (map[string]string, error) {
	if encrypted == "" {
		return map[string]string{}, nil
	}

	plaintext, err := h.cryptoService.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials for masking: %w", err)
	}

	var credsMap map[string]string
	if err := json.Unmarshal([]byte(plaintext), &credsMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	masked := make(map[string]string, len(credsMap))
	for k, v := range credsMap {
		masked[k] = crypto.MaskCredential(v)
	}
	return masked, nil
}

// buildAccountResponse builds the API response for a registrar account, masking credentials.
func (h *RegistrarHandler) buildAccountResponse(account *domain.RegistrarAccount) (*RegistrarAccountResponse, error) {
	maskedCreds, err := h.maskCredentials(account.CredentialsEncrypted)
	if err != nil {
		return nil, err
	}

	return &RegistrarAccountResponse{
		ID:                account.ID,
		RegistrarConfigID: account.RegistrarConfigID,
		RegistrarType:     account.RegistrarConfig.RegistrarType,
		DisplayName:       account.RegistrarConfig.DisplayName,
		AccountName:       account.AccountName,
		Credentials:       maskedCreds,
		Status:            account.Status,
		SyncIntervalHours: account.SyncIntervalHours,
		LastSyncAt:        account.LastSyncAt,
		DomainCount:       account.DomainCount,
		CreatedAt:         account.CreatedAt,
		UpdatedAt:         account.UpdatedAt,
	}, nil
}

// getUserID extracts the authenticated user's ID from the Gin context.
func (h *RegistrarHandler) getUserID(c *gin.Context) uint {
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

// PreviewSync fetches domains from the registrar API without writing to the database.
// Returns the full list of domains as reported by the registrar for user selection.
// POST /api/v1/registrars/:id/preview-sync
func (h *RegistrarHandler) PreviewSync(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	registrarAdapter, err := h.adapterRegistry.Get(account.RegistrarConfig.RegistrarType)
	if err != nil {
		errors.ErrorResponse(c, errors.InternalServer("no adapter found for registrar type"))
		return
	}

	cred, err := h.decryptCredentials(account.CredentialsEncrypted)
	if err != nil {
		h.logger.Error("failed to decrypt credentials", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to decrypt credentials"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	domains, err := registrarAdapter.ListDomains(ctx, cred)
	if err != nil {
		h.logger.Error("failed to list domains for preview", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to fetch domains: "+err.Error()))
		return
	}

	// Build preview response
	type PreviewDomain struct {
		DomainName     string  `json:"domain_name"`
		Status         string  `json:"status"`
		ExpirationDate *string `json:"expiration_date"`
		AutoRenew      bool    `json:"auto_renew"`
		CreationDate   *string `json:"creation_date"`
	}

	preview := make([]PreviewDomain, 0, len(domains))
	for _, d := range domains {
		pd := PreviewDomain{
			DomainName: d.DomainName,
			Status:     d.Status,
			AutoRenew:  d.AutoRenew,
		}
		if d.ExpirationDate != nil {
			s := d.ExpirationDate.Format("2006-01-02")
			pd.ExpirationDate = &s
		}
		if d.CreationDate != nil {
			s := d.CreationDate.Format("2006-01-02")
			pd.CreationDate = &s
		}
		preview = append(preview, pd)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  preview,
		"total": len(preview),
	})
}

// SelectiveImport imports only the user-selected domains from a registrar.
// POST /api/v1/registrars/:id/import
func (h *RegistrarHandler) SelectiveImport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid account ID"))
		return
	}

	var req struct {
		DomainNames []string `json:"domain_names" binding:"required"`
		TagIDs      []uint   `json:"tag_ids"`
		GroupID     *uint    `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("domain_names field is required"))
		return
	}

	if len(req.DomainNames) == 0 {
		errors.ErrorResponse(c, errors.BadRequest("at least one domain must be selected"))
		return
	}

	var account domain.RegistrarAccount
	if err := h.db.Preload("RegistrarConfig").First(&account, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("registrar account not found"))
			return
		}
		h.logger.Error("failed to query registrar account", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query registrar account"))
		return
	}

	registrarAdapter, err := h.adapterRegistry.Get(account.RegistrarConfig.RegistrarType)
	if err != nil {
		errors.ErrorResponse(c, errors.InternalServer("no adapter found for registrar type"))
		return
	}

	cred, err := h.decryptCredentials(account.CredentialsEncrypted)
	if err != nil {
		h.logger.Error("failed to decrypt credentials", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to decrypt credentials"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	allDomains, err := registrarAdapter.ListDomains(ctx, cred)
	if err != nil {
		h.logger.Error("failed to list domains for import", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to fetch domains"))
		return
	}

	// Filter to only selected domains
	selectedSet := make(map[string]bool, len(req.DomainNames))
	for _, name := range req.DomainNames {
		selectedSet[name] = true
	}

	var selected []domain.NormalizedDomain
	for _, d := range allDomains {
		if selectedSet[d.DomainName] {
			selected = append(selected, d)
		}
	}

	// Import using MergeDomains from the sync package (inline merge logic)
	accountID := account.ID
	now := time.Now()
	imported := 0
	for _, remote := range selected {
		var existing domain.NormalizedDomain
		result := h.db.Where("domain_name = ?", remote.DomainName).First(&existing)
		if result.Error == gorm.ErrRecordNotFound {
			newDomain := remote
			newDomain.DataSourceType = "api"
			newDomain.RegistrarAccountID = &accountID
			newDomain.LastSyncAt = &now
			newDomain.GroupID = req.GroupID
			if createErr := h.db.Create(&newDomain).Error; createErr != nil {
				h.logger.Error("failed to import domain", zap.String("domain", remote.DomainName), zap.Error(createErr))
				continue
			}
			// Assign tags if specified
			if len(req.TagIDs) > 0 {
				var tags []domain.Tag
				h.db.Where("id IN ?", req.TagIDs).Find(&tags)
				h.db.Model(&newDomain).Association("Tags").Replace(tags)
			}
			imported++
		} else if result.Error == nil {
			// Already exists, update
			updates := map[string]interface{}{
				"expiration_date":      remote.ExpirationDate,
				"auto_renew":           remote.AutoRenew,
				"status":               remote.Status,
				"registrar_account_id": accountID,
				"last_sync_at":         now,
			}
			if req.GroupID != nil {
				updates["group_id"] = *req.GroupID
			}
			h.db.Model(&existing).Updates(updates)
			// Assign tags if specified
			if len(req.TagIDs) > 0 {
				var tags []domain.Tag
				h.db.Where("id IN ?", req.TagIDs).Find(&tags)
				h.db.Model(&existing).Association("Tags").Replace(tags)
			}
			imported++
		}
	}

	// Update account domain count
	var count int64
	h.db.Model(&domain.NormalizedDomain{}).Where("registrar_account_id = ?", accountID).Count(&count)
	h.db.Model(&account).Update("domain_count", count)
	h.db.Model(&account).Update("last_sync_at", now)

	c.JSON(http.StatusOK, gin.H{
		"message":  "import completed",
		"imported": imported,
		"total":    len(selected),
	})
}
