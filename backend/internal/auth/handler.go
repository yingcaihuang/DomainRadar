package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"domainradar/internal/domain"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

// AuthHandler implements the OIDC authentication endpoints.
type AuthHandler struct {
	OIDCProvider   *OIDCProvider
	SessionManager *SessionManager
	DB             *gorm.DB
	Logger         *zap.Logger
	DevMode        bool
}

// NewAuthHandler creates a new AuthHandler with the given dependencies.
func NewAuthHandler(provider *OIDCProvider, sm *SessionManager, db *gorm.DB, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{
		OIDCProvider:   provider,
		SessionManager: sm,
		DB:             db,
		Logger:         logger,
		DevMode:        provider == nil,
	}
}

// HandleLogin initiates the OIDC authentication flow by redirecting to the SSO provider.
// GET /api/v1/auth/login-sso
func (h *AuthHandler) HandleLogin(c *gin.Context) {
	if h.DevMode {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "SSO is not configured. Use POST /api/v1/auth/dev-login instead.",
			"mode":  "dev",
		})
		return
	}

	state, err := generateState()
	if err != nil {
		h.Logger.Error("Failed to generate OAuth2 state", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initiate login"})
		return
	}

	// Store state in a short-lived cookie for CSRF protection
	c.SetCookie("oauth_state", state, 300, "/", "", false, true)

	url := h.OIDCProvider.OAuth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// HandleCallback handles the OIDC callback after Authentik authentication.
// GET /api/v1/auth/callback
func (h *AuthHandler) HandleCallback(c *gin.Context) {
	// Verify state parameter
	stateCookie, err := c.Cookie("oauth_state")
	if err != nil {
		h.Logger.Warn("Missing OAuth2 state cookie")
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing state cookie"})
		return
	}

	stateParam := c.Query("state")
	if stateParam == "" || stateParam != stateCookie {
		h.Logger.Warn("OAuth2 state mismatch", zap.String("expected", stateCookie), zap.String("got", stateParam))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state parameter"})
		return
	}

	// Clear the state cookie
	c.SetCookie("oauth_state", "", -1, "/", "", false, true)

	// Exchange the authorization code for tokens
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	oauth2Token, err := h.OIDCProvider.OAuth2Config.Exchange(c.Request.Context(), code)
	if err != nil {
		h.Logger.Error("Failed to exchange authorization code", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange code"})
		return
	}

	// Extract and verify the ID token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		h.Logger.Error("No id_token in OAuth2 response")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no id_token in response"})
		return
	}

	idToken, err := h.OIDCProvider.Verifier.Verify(c.Request.Context(), rawIDToken)
	if err != nil {
		h.Logger.Error("Failed to verify ID token", zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid id_token"})
		return
	}

	// Parse claims from the ID token
	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		h.Logger.Error("Failed to parse ID token claims", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse claims"})
		return
	}

	// Map Authentik groups to internal roles (check DB mappings first, fallback to string matching)
	roles := ResolveGroupRoles(h.DB, claims.Groups)
	if roles == nil {
		roles = MapGroupsToRoles(claims.Groups)
	}

	// Create or update user in the database
	user, err := h.upsertUser(claims, roles)
	if err != nil {
		h.Logger.Error("Failed to upsert user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// Create a session
	session, err := h.SessionManager.CreateSession(user.ID, user.Email, user.DisplayName, roles)
	if err != nil {
		h.Logger.Error("Failed to create session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	// Set session token as a cookie
	c.SetCookie("session_token", session.Token, int(h.SessionManager.ttl.Seconds()), "/", "", false, true)
	h.Logger.Info("User logged in via SSO", zap.String("email", user.Email), zap.Strings("roles", roles))

	// Redirect to frontend after successful SSO login
	c.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}

// HandleLogout destroys the current session.
// POST /api/v1/auth/logout
func (h *AuthHandler) HandleLogout(c *gin.Context) {
	token := extractSessionToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	h.SessionManager.DeleteSession(token)

	// Clear session cookie
	c.SetCookie("session_token", "", -1, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// HandleMe returns the current authenticated user's information.
// GET /api/v1/auth/me
func (h *AuthHandler) HandleMe(c *gin.Context) {
	token := extractSessionToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	session := h.SessionManager.GetSession(token)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired or invalid"})
		return
	}

	// Fetch user from DB to get must_change_password status
	var user domain.User
	mustChange := false
	if err := h.DB.First(&user, session.UserID).Error; err == nil {
		mustChange = user.MustChangePassword
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":                   session.UserID,
			"email":                session.Email,
			"display_name":         session.Name,
			"roles":                session.Roles,
			"must_change_password": mustChange,
		},
	})
}

// HandleDevLogin provides a development-only login that creates a default admin user.
// This only works when DevMode is true (i.e. OIDC is not configured).
// POST /api/v1/auth/dev-login
func (h *AuthHandler) HandleDevLogin(c *gin.Context) {
	if !h.DevMode {
		c.JSON(http.StatusForbidden, gin.H{"error": "dev login is not available in production"})
		return
	}

	// Upsert the default dev admin user
	var user domain.User
	result := h.DB.Where("external_id = ?", "dev-admin").First(&user)
	now := time.Now()

	if result.Error == gorm.ErrRecordNotFound {
		user = domain.User{
			ExternalID:  "dev-admin",
			Email:       "admin@localhost",
			DisplayName: "Dev Admin",
			LastLoginAt: &now,
		}
		if err := h.DB.Create(&user).Error; err != nil {
			h.Logger.Error("Failed to create dev admin user", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create dev user"})
			return
		}
	} else if result.Error != nil {
		h.Logger.Error("Failed to query dev admin user", zap.Error(result.Error))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	} else {
		user.LastLoginAt = &now
		h.DB.Save(&user)
	}

	// Ensure admin role exists
	h.DB.Where("user_id = ?", user.ID).Delete(&domain.UserRole{})
	h.DB.Create(&domain.UserRole{UserID: user.ID, Role: RoleAdmin})

	roles := []string{RoleAdmin}

	// Create session
	session, err := h.SessionManager.CreateSession(user.ID, user.Email, user.DisplayName, roles)
	if err != nil {
		h.Logger.Error("Failed to create dev session", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	// Set session cookie
	c.SetCookie("session_token", session.Token, int(h.SessionManager.ttl.Seconds()), "/", "", false, true)

	h.Logger.Info("Dev admin logged in", zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "dev login successful",
		"user": gin.H{
			"id":           user.ID,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"roles":        roles,
		},
		"token": session.Token,
	})
}

// HandleAuthMode returns authentication mode status.
// GET /api/v1/auth/mode
func (h *AuthHandler) HandleAuthMode(c *gin.Context) {
	ssoEnabled := false
	if h.OIDCProvider != nil && !h.DevMode {
		ssoEnabled = true
	} else {
		// Also check DB config
		var config domain.SSOConfig
		if err := h.DB.First(&config).Error; err == nil && config.Enabled {
			ssoEnabled = true
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"sso_enabled":   ssoEnabled,
		"local_enabled": true,
	})
}

// RegisterRoutes registers the auth routes on a Gin router group.
func (h *AuthHandler) RegisterRoutes(group *gin.RouterGroup) {
	auth := group.Group("/auth")
	{
		auth.GET("/mode", h.HandleAuthMode)
		auth.POST("/login", h.HandleLocalLogin)
		auth.GET("/login-sso", h.HandleLogin)
		auth.GET("/callback", h.HandleCallback)
		auth.POST("/logout", h.HandleLogout)
		auth.GET("/me", h.HandleMe)
		auth.POST("/change-password", h.HandleChangePassword)
		auth.POST("/dev-login", h.HandleDevLogin)
	}
}

// MapGroupsToRoles maps Authentik groups/claims to internal DomainRadar roles.
// The mapping checks for "admin", "operator", "viewer" in group names (case-insensitive).
// If no recognized group is found, the user defaults to "viewer" role.
func MapGroupsToRoles(groups []string) []string {
	roleSet := make(map[string]bool)

	for _, group := range groups {
		lower := strings.ToLower(group)
		switch {
		case strings.Contains(lower, "admin"):
			roleSet["admin"] = true
		case strings.Contains(lower, "operator"):
			roleSet["operator"] = true
		case strings.Contains(lower, "viewer"):
			roleSet["viewer"] = true
		}
	}

	// If no roles were matched, default to viewer
	if len(roleSet) == 0 {
		return []string{"viewer"}
	}

	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	return roles
}

// upsertUser creates or updates a user record based on OIDC claims.
func (h *AuthHandler) upsertUser(claims Claims, roles []string) (*domain.User, error) {
	var user domain.User

	// Try to find by Sub first, then by email (for SSO users created with UUID as external_id)
	result := h.DB.Where("external_id = ? OR (email = ? AND auth_source = 'oidc')", claims.Sub, claims.Email).First(&user)
	now := time.Now()

	if result.Error == gorm.ErrRecordNotFound {
		// Create new user - use preferred_username or email as readable external_id
		externalID := claims.PreferredUser
		if externalID == "" {
			externalID = claims.Email
		}
		if externalID == "" {
			externalID = claims.Sub
		}
		user = domain.User{
			ExternalID:  externalID,
			Email:       claims.Email,
			DisplayName: claims.Name,
			AuthSource:  "oidc",
			LastLoginAt: &now,
		}
		if err := h.DB.Create(&user).Error; err != nil {
			return nil, err
		}
	} else if result.Error != nil {
		return nil, result.Error
	} else {
		// Update existing user
		user.Email = claims.Email
		user.DisplayName = claims.Name
		user.AuthSource = "oidc"
		user.LastLoginAt = &now
		if err := h.DB.Save(&user).Error; err != nil {
			return nil, err
		}
	}

	// Sync roles: delete existing and recreate
	if err := h.DB.Where("user_id = ?", user.ID).Delete(&domain.UserRole{}).Error; err != nil {
		return nil, err
	}
	for _, role := range roles {
		userRole := domain.UserRole{
			UserID: user.ID,
			Role:   role,
		}
		if err := h.DB.Create(&userRole).Error; err != nil {
			return nil, err
		}
	}

	return &user, nil
}

// extractSessionToken extracts the session token from the Authorization header or cookie.
func extractSessionToken(c *gin.Context) string {
	// Check Authorization header first (Bearer token)
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return parts[1]
		}
	}

	// Fall back to cookie
	token, _ := c.Cookie("session_token")
	return token
}

// generateState creates a random state string for OAuth2 CSRF protection.
func generateState() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
