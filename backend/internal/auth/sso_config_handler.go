package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SSOConfigHandler implements SSO configuration management endpoints.
type SSOConfigHandler struct {
	DB     *gorm.DB
	Logger *zap.Logger

	// mu protects the live OIDC provider reference for runtime re-initialization.
	mu           sync.RWMutex
	authHandler  *AuthHandler
}

// NewSSOConfigHandler creates a new SSOConfigHandler.
func NewSSOConfigHandler(db *gorm.DB, logger *zap.Logger, authHandler *AuthHandler) *SSOConfigHandler {
	return &SSOConfigHandler{
		DB:          db,
		Logger:      logger,
		authHandler: authHandler,
	}
}

// ssoConfigResponse is the API response for SSO config (masks secret).
type ssoConfigResponse struct {
	ID                    uint   `json:"id"`
	Enabled               bool   `json:"enabled"`
	IssuerURL             string `json:"issuer_url"`
	DiscoveryURL          string `json:"discovery_url"`
	ClientID              string `json:"client_id"`
	HasSecret             bool   `json:"has_secret"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
	RedirectURL           string `json:"redirect_url"`
	Scopes                string `json:"scopes"`
	GroupsClaim           string `json:"groups_claim"`
	GroupsSource          string `json:"groups_source"`
	ShowOnLoginPage       bool   `json:"show_on_login_page"`
	CookieSecure          bool   `json:"cookie_secure"`
	UpdatedAt             string `json:"updated_at"`
}

// ssoConfigUpdateRequest is the request body for updating SSO config.
type ssoConfigUpdateRequest struct {
	Enabled               bool   `json:"enabled"`
	IssuerURL             string `json:"issuer_url"`
	DiscoveryURL          string `json:"discovery_url"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret,omitempty"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
	RedirectURL           string `json:"redirect_url"`
	Scopes                string `json:"scopes"`
	GroupsClaim           string `json:"groups_claim"`
	GroupsSource          string `json:"groups_source"`
	ShowOnLoginPage       bool   `json:"show_on_login_page"`
	CookieSecure          bool   `json:"cookie_secure"`
}

// HandleGetSSOConfig returns the current SSO configuration (admin only).
// GET /api/v1/config/sso
func (h *SSOConfigHandler) HandleGetSSOConfig(c *gin.Context) {
	var config domain.SSOConfig
	result := h.DB.First(&config)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		h.Logger.Error("Failed to get SSO config", zap.Error(result.Error))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve SSO configuration"))
		return
	}

	if result.Error == gorm.ErrRecordNotFound {
		// Return empty config
		c.JSON(http.StatusOK, gin.H{
			"data": ssoConfigResponse{
				Enabled:   false,
				HasSecret: false,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": toSSOConfigResponse(config),
	})
}

// HandleUpdateSSOConfig updates the SSO configuration (admin only).
// PUT /api/v1/config/sso
func (h *SSOConfigHandler) HandleUpdateSSOConfig(c *gin.Context) {
	var req ssoConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body"))
		return
	}

	// Validate required fields if enabled
	if req.Enabled {
		if req.IssuerURL == "" || req.ClientID == "" || req.RedirectURL == "" {
			errors.ErrorResponse(c, errors.BadRequest("issuer_url, client_id, and redirect_url are required when SSO is enabled"))
			return
		}
	}

	// Upsert the config (there's only one row)
	var config domain.SSOConfig
	result := h.DB.First(&config)

	if result.Error == gorm.ErrRecordNotFound {
		config = domain.SSOConfig{
			Enabled:               req.Enabled,
			IssuerURL:             req.IssuerURL,
			DiscoveryURL:          req.DiscoveryURL,
			ClientID:              req.ClientID,
			ClientSecret:          req.ClientSecret,
			AuthorizationEndpoint: req.AuthorizationEndpoint,
			TokenEndpoint:         req.TokenEndpoint,
			UserinfoEndpoint:      req.UserinfoEndpoint,
			JWKSURI:               req.JWKSURI,
			EndSessionEndpoint:    req.EndSessionEndpoint,
			RedirectURL:           req.RedirectURL,
			Scopes:                req.Scopes,
			GroupsClaim:           req.GroupsClaim,
			GroupsSource:          req.GroupsSource,
			ShowOnLoginPage:       req.ShowOnLoginPage,
			CookieSecure:          req.CookieSecure,
		}
		if err := h.DB.Create(&config).Error; err != nil {
			h.Logger.Error("Failed to create SSO config", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to save SSO configuration"))
			return
		}
	} else if result.Error != nil {
		h.Logger.Error("Failed to query SSO config", zap.Error(result.Error))
		errors.ErrorResponse(c, errors.InternalServer("failed to save SSO configuration"))
		return
	} else {
		config.Enabled = req.Enabled
		config.IssuerURL = req.IssuerURL
		config.DiscoveryURL = req.DiscoveryURL
		config.ClientID = req.ClientID
		config.AuthorizationEndpoint = req.AuthorizationEndpoint
		config.TokenEndpoint = req.TokenEndpoint
		config.UserinfoEndpoint = req.UserinfoEndpoint
		config.JWKSURI = req.JWKSURI
		config.EndSessionEndpoint = req.EndSessionEndpoint
		config.RedirectURL = req.RedirectURL
		config.Scopes = req.Scopes
		config.GroupsClaim = req.GroupsClaim
		config.GroupsSource = req.GroupsSource
		config.ShowOnLoginPage = req.ShowOnLoginPage
		config.CookieSecure = req.CookieSecure
		if req.ClientSecret != "" {
			config.ClientSecret = req.ClientSecret
		}
		if err := h.DB.Save(&config).Error; err != nil {
			h.Logger.Error("Failed to update SSO config", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to save SSO configuration"))
			return
		}
	}

	// If enabled, try to re-initialize the OIDC provider at runtime
	if config.Enabled && config.IssuerURL != "" && config.ClientID != "" {
		go h.reinitializeOIDC(config)
	} else if !config.Enabled {
		// Disable OIDC provider
		h.mu.Lock()
		h.authHandler.OIDCProvider = nil
		h.authHandler.DevMode = true
		h.mu.Unlock()
	}

	h.Logger.Info("SSO config updated", zap.Bool("enabled", config.Enabled))

	c.JSON(http.StatusOK, gin.H{
		"message": "SSO configuration saved",
		"data": toSSOConfigResponse(config),
	})
}

// HandleTestSSOConfig tests OIDC connectivity by fetching the .well-known/openid-configuration endpoint.
// POST /api/v1/config/sso/test
func (h *SSOConfigHandler) HandleTestSSOConfig(c *gin.Context) {
	var req ssoConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body"))
		return
	}

	if req.IssuerURL == "" {
		errors.ErrorResponse(c, errors.BadRequest("issuer_url is required for testing"))
		return
	}

	// Normalize issuer URL
	issuerURL := strings.TrimRight(req.IssuerURL, "/")
	wellKnownURL := issuerURL + "/.well-known/openid-configuration"

	// Fetch the well-known endpoint
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(wellKnownURL)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to connect: %v", err),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("OIDC discovery endpoint returned HTTP %d", resp.StatusCode),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "OIDC provider is reachable and responding",
	})
}

// reinitializeOIDC attempts to create a new OIDC provider from the stored config.
func (h *SSOConfigHandler) reinitializeOIDC(config domain.SSOConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	provider, err := NewOIDCProvider(ctx, OIDCConfig{
		IssuerURL:    config.IssuerURL,
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
	})
	if err != nil {
		h.Logger.Warn("Failed to re-initialize OIDC provider from DB config", zap.Error(err))
		return
	}

	h.mu.Lock()
	h.authHandler.OIDCProvider = provider
	h.authHandler.DevMode = false
	h.mu.Unlock()

	h.Logger.Info("OIDC provider re-initialized from database config")
}

// InitializeOIDCFromDB loads SSO config from the database and initializes the OIDC provider if enabled.
// Called at startup to restore SSO state from DB.
func InitializeOIDCFromDB(db *gorm.DB, authHandler *AuthHandler, logger *zap.Logger) {
	var config domain.SSOConfig
	result := db.First(&config)
	if result.Error != nil {
		// No config or DB error — leave auth handler in current state
		return
	}

	if !config.Enabled || config.IssuerURL == "" || config.ClientID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	provider, err := NewOIDCProvider(ctx, OIDCConfig{
		IssuerURL:    config.IssuerURL,
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
	})
	if err != nil {
		logger.Warn("Failed to initialize OIDC provider from DB config at startup", zap.Error(err))
		return
	}

	authHandler.OIDCProvider = provider
	authHandler.DevMode = false
	logger.Info("OIDC provider initialized from database configuration")
}

// HandleDiscoverOIDC fetches the .well-known/openid-configuration and returns parsed endpoints.
// POST /api/v1/config/sso/discover
func (h *SSOConfigHandler) HandleDiscoverOIDC(c *gin.Context) {
	var req struct {
		IssuerURL string `json:"issuer_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("issuer_url is required"))
		return
	}

	issuerURL := strings.TrimRight(req.IssuerURL, "/")
	wellKnownURL := issuerURL + "/.well-known/openid-configuration"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(wellKnownURL)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("无法连接: %v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("返回 HTTP %d", resp.StatusCode)})
		return
	}

	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		UserinfoEndpoint      string `json:"userinfo_endpoint"`
		JWKSURI               string `json:"jwks_uri"`
		EndSessionEndpoint    string `json:"end_session_endpoint"`
	}

	import_json := func() error {
		decoder := json.NewDecoder(resp.Body)
		return decoder.Decode(&discovery)
	}
	if err := import_json(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无法解析 OIDC 发现文档"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"issuer":                 discovery.Issuer,
			"authorization_endpoint": discovery.AuthorizationEndpoint,
			"token_endpoint":         discovery.TokenEndpoint,
			"userinfo_endpoint":      discovery.UserinfoEndpoint,
			"jwks_uri":               discovery.JWKSURI,
			"end_session_endpoint":   discovery.EndSessionEndpoint,
			"discovery_url":          wellKnownURL,
		},
	})
}

func toSSOConfigResponse(config domain.SSOConfig) ssoConfigResponse {
	return ssoConfigResponse{
		ID:                    config.ID,
		Enabled:               config.Enabled,
		IssuerURL:             config.IssuerURL,
		DiscoveryURL:          config.DiscoveryURL,
		ClientID:              config.ClientID,
		HasSecret:             config.ClientSecret != "",
		AuthorizationEndpoint: config.AuthorizationEndpoint,
		TokenEndpoint:         config.TokenEndpoint,
		UserinfoEndpoint:      config.UserinfoEndpoint,
		JWKSURI:               config.JWKSURI,
		EndSessionEndpoint:    config.EndSessionEndpoint,
		RedirectURL:           config.RedirectURL,
		Scopes:                config.Scopes,
		GroupsClaim:           config.GroupsClaim,
		GroupsSource:          config.GroupsSource,
		ShowOnLoginPage:       config.ShowOnLoginPage,
		CookieSecure:          config.CookieSecure,
		UpdatedAt:             config.UpdatedAt.Format(time.RFC3339),
	}
}

// RegisterRoutes registers SSO config management routes on the given router group.
// All SSO config endpoints require admin role.
func (h *SSOConfigHandler) RegisterRoutes(group *gin.RouterGroup) {
	sso := group.Group("/config/sso")
	sso.Use(RequireRole(RoleAdmin))
	{
		sso.GET("", h.HandleGetSSOConfig)
		sso.PUT("", h.HandleUpdateSSOConfig)
		sso.POST("/test", h.HandleTestSSOConfig)
		sso.POST("/discover", h.HandleDiscoverOIDC)
	}
}
