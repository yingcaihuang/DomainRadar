package auth

import (
	"github.com/gin-gonic/gin"

	"domainradar/internal/errors"
)

// Role constants define the available roles in the system.
const (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"
)

// Permission constants define the available permissions.
const (
	ViewDomains            = "view_domains"
	ManageDomains          = "manage_domains"
	ViewAlerts             = "view_alerts"
	ManageAlerts           = "manage_alerts"
	ConfigureIntegrations  = "configure_integrations"
	ManageUsers            = "manage_users"
	ViewAuditLogs          = "view_audit_logs"
)

// permissionMatrix maps each role to its granted permissions.
var permissionMatrix = map[string]map[string]bool{
	RoleViewer: {
		ViewDomains: true,
		ViewAlerts:  true,
	},
	RoleOperator: {
		ViewDomains:   true,
		ManageDomains: true,
		ViewAlerts:    true,
		ManageAlerts:  true,
	},
	RoleAdmin: {
		ViewDomains:           true,
		ManageDomains:         true,
		ViewAlerts:            true,
		ManageAlerts:          true,
		ConfigureIntegrations: true,
		ManageUsers:           true,
		ViewAuditLogs:         true,
	},
}

// UserInfo represents the authenticated user information stored in the Gin context.
type UserInfo struct {
	ID          uint     `json:"id"`
	ExternalID  string   `json:"external_id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}

// HasPermission checks whether any of the given roles grant the specified permission.
func HasPermission(roles []string, permission string) bool {
	for _, role := range roles {
		if perms, ok := permissionMatrix[role]; ok {
			if perms[permission] {
				return true
			}
		}
	}
	return false
}

// RequirePermission returns a Gin middleware that checks if the authenticated user
// has any role that grants the specified permission. Returns HTTP 403 if not.
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userVal, exists := c.Get("user")
		if !exists {
			errors.ErrorResponse(c, errors.Unauthorized("authentication required"))
			c.Abort()
			return
		}

		user, ok := userVal.(*UserInfo)
		if !ok {
			errors.ErrorResponse(c, errors.InternalServer("invalid user context"))
			c.Abort()
			return
		}

		if !HasPermission(user.Roles, permission) {
			errors.ErrorResponse(c, errors.Forbidden("insufficient permissions"))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireRole returns a Gin middleware that checks if the authenticated user
// has at least one of the specified roles. Returns HTTP 403 if not.
func RequireRole(roles ...string) gin.HandlerFunc {
	requiredSet := make(map[string]bool, len(roles))
	for _, r := range roles {
		requiredSet[r] = true
	}

	return func(c *gin.Context) {
		userVal, exists := c.Get("user")
		if !exists {
			errors.ErrorResponse(c, errors.Unauthorized("authentication required"))
			c.Abort()
			return
		}

		user, ok := userVal.(*UserInfo)
		if !ok {
			errors.ErrorResponse(c, errors.InternalServer("invalid user context"))
			c.Abort()
			return
		}

		for _, userRole := range user.Roles {
			if requiredSet[userRole] {
				c.Next()
				return
			}
		}

		errors.ErrorResponse(c, errors.Forbidden("insufficient role privileges"))
		c.Abort()
	}
}
