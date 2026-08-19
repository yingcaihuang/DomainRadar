package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// helper to create a test router with user context set.
func setupRouter(user *UserInfo, middleware gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if user != nil {
			c.Set("user", user)
		}
		c.Next()
	})
	r.Use(middleware)
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

// errorBody is used to parse JSON error responses in tests.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestHasPermission(t *testing.T) {
	tests := []struct {
		name       string
		roles      []string
		permission string
		expected   bool
	}{
		// Viewer permissions
		{"viewer can view domains", []string{RoleViewer}, ViewDomains, true},
		{"viewer can view alerts", []string{RoleViewer}, ViewAlerts, true},
		{"viewer cannot manage domains", []string{RoleViewer}, ManageDomains, false},
		{"viewer cannot manage alerts", []string{RoleViewer}, ManageAlerts, false},
		{"viewer cannot configure integrations", []string{RoleViewer}, ConfigureIntegrations, false},
		{"viewer cannot manage users", []string{RoleViewer}, ManageUsers, false},
		{"viewer cannot view audit logs", []string{RoleViewer}, ViewAuditLogs, false},

		// Operator permissions
		{"operator can view domains", []string{RoleOperator}, ViewDomains, true},
		{"operator can manage domains", []string{RoleOperator}, ManageDomains, true},
		{"operator can view alerts", []string{RoleOperator}, ViewAlerts, true},
		{"operator can manage alerts", []string{RoleOperator}, ManageAlerts, true},
		{"operator cannot configure integrations", []string{RoleOperator}, ConfigureIntegrations, false},
		{"operator cannot manage users", []string{RoleOperator}, ManageUsers, false},
		{"operator cannot view audit logs", []string{RoleOperator}, ViewAuditLogs, false},

		// Admin permissions
		{"admin can view domains", []string{RoleAdmin}, ViewDomains, true},
		{"admin can manage domains", []string{RoleAdmin}, ManageDomains, true},
		{"admin can view alerts", []string{RoleAdmin}, ViewAlerts, true},
		{"admin can manage alerts", []string{RoleAdmin}, ManageAlerts, true},
		{"admin can configure integrations", []string{RoleAdmin}, ConfigureIntegrations, true},
		{"admin can manage users", []string{RoleAdmin}, ManageUsers, true},
		{"admin can view audit logs", []string{RoleAdmin}, ViewAuditLogs, true},

		// Edge cases
		{"empty roles has no permissions", []string{}, ViewDomains, false},
		{"unknown role has no permissions", []string{"unknown"}, ViewDomains, false},
		{"multiple roles - highest wins", []string{RoleViewer, RoleOperator}, ManageDomains, true},
		{"nil roles has no permissions", nil, ViewDomains, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := HasPermission(tc.roles, tc.permission)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRequirePermission_Allowed(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleOperator},
	}
	router := setupRouter(user, RequirePermission(ManageDomains))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequirePermission_Forbidden(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleViewer},
	}
	router := setupRouter(user, RequirePermission(ManageDomains))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var body errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "FORBIDDEN", body.Error.Code)
	assert.Equal(t, "insufficient permissions", body.Error.Message)
}

func TestRequirePermission_NoUser(t *testing.T) {
	router := setupRouter(nil, RequirePermission(ViewDomains))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "UNAUTHORIZED", body.Error.Code)
}

func TestRequireRole_Allowed(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleAdmin},
	}
	router := setupRouter(user, RequireRole(RoleAdmin))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_MultipleAcceptedRoles(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleOperator},
	}
	router := setupRouter(user, RequireRole(RoleOperator, RoleAdmin))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_Forbidden(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleViewer},
	}
	router := setupRouter(user, RequireRole(RoleAdmin))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var body errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "FORBIDDEN", body.Error.Code)
	assert.Equal(t, "insufficient role privileges", body.Error.Message)
}

func TestRequireRole_NoUser(t *testing.T) {
	router := setupRouter(nil, RequireRole(RoleAdmin))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequirePermission_AdminAllEndpoints(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleAdmin},
	}

	allPermissions := []string{
		ViewDomains, ManageDomains, ViewAlerts, ManageAlerts,
		ConfigureIntegrations, ManageUsers, ViewAuditLogs,
	}

	for _, perm := range allPermissions {
		t.Run(perm, func(t *testing.T) {
			router := setupRouter(user, RequirePermission(perm))
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestRequirePermission_ViewerOnlyViewEndpoints(t *testing.T) {
	user := &UserInfo{
		ID:    1,
		Roles: []string{RoleViewer},
	}

	allowedPerms := []string{ViewDomains, ViewAlerts}
	deniedPerms := []string{ManageDomains, ManageAlerts, ConfigureIntegrations, ManageUsers, ViewAuditLogs}

	for _, perm := range allowedPerms {
		t.Run("allowed_"+perm, func(t *testing.T) {
			router := setupRouter(user, RequirePermission(perm))
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}

	for _, perm := range deniedPerms {
		t.Run("denied_"+perm, func(t *testing.T) {
			router := setupRouter(user, RequirePermission(perm))
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}
