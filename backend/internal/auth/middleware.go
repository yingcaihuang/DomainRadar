package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Context keys for storing authenticated user data.
const (
	ContextKeyUserID = "auth_user_id"
	ContextKeyEmail  = "auth_email"
	ContextKeyName   = "auth_name"
	ContextKeyRoles  = "auth_roles"
)

// AuthMiddleware creates a Gin middleware that validates session tokens
// and sets user information in the request context.
func AuthMiddleware(sm *SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractSessionToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}

		session := sm.GetSession(token)
		if session == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "session expired or invalid",
			})
			return
		}

		// Set user information in context for downstream handlers
		c.Set(ContextKeyUserID, session.UserID)
		c.Set(ContextKeyEmail, session.Email)
		c.Set(ContextKeyName, session.Name)
		c.Set(ContextKeyRoles, session.Roles)

		// Set unified user object for RBAC middleware (RequirePermission/RequireRole)
		c.Set("user", &UserInfo{
			ID:          session.UserID,
			Email:       session.Email,
			DisplayName: session.Name,
			Roles:       session.Roles,
		})

		c.Next()
	}
}

// GetUserID extracts the authenticated user's ID from the Gin context.
func GetUserID(c *gin.Context) (uint, bool) {
	val, exists := c.Get(ContextKeyUserID)
	if !exists {
		return 0, false
	}
	id, ok := val.(uint)
	return id, ok
}

// GetUserRoles extracts the authenticated user's roles from the Gin context.
func GetUserRoles(c *gin.Context) []string {
	val, exists := c.Get(ContextKeyRoles)
	if !exists {
		return nil
	}
	roles, ok := val.([]string)
	if !ok {
		return nil
	}
	return roles
}

// GetUserEmail extracts the authenticated user's email from the Gin context.
func GetUserEmail(c *gin.Context) string {
	val, exists := c.Get(ContextKeyEmail)
	if !exists {
		return ""
	}
	email, ok := val.(string)
	if !ok {
		return ""
	}
	return email
}
