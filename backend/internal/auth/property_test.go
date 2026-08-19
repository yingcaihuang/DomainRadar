package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"pgregory.net/rapid"
)

// **Validates: Requirements 10.4**
// Property 23: RBAC enforcement — For any (role, endpoint) combination, return 403 iff
// role lacks required permission; decisions consistent with permission matrix.

// allValidRoles includes the three defined roles.
var allValidRoles = []string{RoleViewer, RoleOperator, RoleAdmin}

// allValidPermissions includes the seven defined permissions.
var allValidPermissions = []string{
	ViewDomains, ManageDomains, ViewAlerts, ManageAlerts,
	ConfigureIntegrations, ManageUsers, ViewAuditLogs,
}

// roleHierarchy defines the role containment order: admin ⊇ operator ⊇ viewer.
var roleHierarchy = []string{RoleViewer, RoleOperator, RoleAdmin}

// genRoles generates a random subset of roles including valid roles, empty, and unknown values.
func genRoles(t *rapid.T) []string {
	candidates := append([]string{}, allValidRoles...)
	candidates = append(candidates, "", "unknown", "superadmin", "readonly")

	n := rapid.IntRange(0, 4).Draw(t, "numRoles")
	roles := make([]string, 0, n)
	for i := 0; i < n; i++ {
		idx := rapid.IntRange(0, len(candidates)-1).Draw(t, "roleIdx")
		roles = append(roles, candidates[idx])
	}
	return roles
}

// genPermission generates a random permission from the valid set.
func genPermission(t *rapid.T) string {
	idx := rapid.IntRange(0, len(allValidPermissions)-1).Draw(t, "permIdx")
	return allValidPermissions[idx]
}

// expectedHasPermission computes the expected result of HasPermission by consulting
// the permission matrix directly.
func expectedHasPermission(roles []string, permission string) bool {
	for _, role := range roles {
		if perms, ok := permissionMatrix[role]; ok {
			if perms[permission] {
				return true
			}
		}
	}
	return false
}

// TestProperty_HasPermissionMatchesMatrix verifies that HasPermission returns true
// if and only if the permission matrix grants that permission for any of the roles.
func TestProperty_HasPermissionMatchesMatrix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		roles := genRoles(t)
		perm := genPermission(t)

		got := HasPermission(roles, perm)
		want := expectedHasPermission(roles, perm)

		if got != want {
			t.Fatalf("HasPermission(%v, %q) = %v, want %v", roles, perm, got, want)
		}
	})
}

// TestProperty_MiddlewareReturns403IffUnauthorized verifies that the RequirePermission
// middleware returns HTTP 200 iff HasPermission is true, and HTTP 403 otherwise.
func TestProperty_MiddlewareReturns403IffUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rapid.Check(t, func(t *rapid.T) {
		roles := genRoles(t)
		perm := genPermission(t)

		user := &UserInfo{
			ID:    1,
			Email: "test@example.com",
			Roles: roles,
		}

		// Build a test router with the RequirePermission middleware.
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user", user)
			c.Next()
		})
		r.Use(RequirePermission(perm))
		r.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)

		allowed := HasPermission(roles, perm)

		if allowed && w.Code != http.StatusOK {
			t.Fatalf("HasPermission(%v, %q) = true but middleware returned %d, want 200",
				roles, perm, w.Code)
		}
		if !allowed && w.Code != http.StatusForbidden {
			t.Fatalf("HasPermission(%v, %q) = false but middleware returned %d, want 403",
				roles, perm, w.Code)
		}
	})
}

// TestProperty_RoleHierarchyConsistency verifies that adding a higher role never
// removes access: admin ⊇ operator ⊇ viewer.
// For every permission, if a lower role grants it, all higher roles must also grant it.
func TestProperty_RoleHierarchyConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		perm := genPermission(t)

		// Check that the hierarchy is monotonically non-decreasing in permissions.
		// For each consecutive pair in the hierarchy, if the lower role has a permission,
		// the higher role must also have it.
		for i := 0; i < len(roleHierarchy)-1; i++ {
			lowerRole := roleHierarchy[i]
			higherRole := roleHierarchy[i+1]

			lowerHas := HasPermission([]string{lowerRole}, perm)
			higherHas := HasPermission([]string{higherRole}, perm)

			if lowerHas && !higherHas {
				t.Fatalf("role hierarchy violated: %q has permission %q but higher role %q does not",
					lowerRole, perm, higherRole)
			}
		}
	})
}

// TestProperty_EmptyAndUnknownRolesNeverGrantAccess verifies that empty or unknown
// roles never grant any permission.
func TestProperty_EmptyAndUnknownRolesNeverGrantAccess(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate roles from only invalid/unknown values.
		invalidRoles := []string{"", "unknown", "superadmin", "readonly", "guest", "moderator"}
		n := rapid.IntRange(0, 3).Draw(t, "numRoles")
		roles := make([]string, 0, n)
		for i := 0; i < n; i++ {
			idx := rapid.IntRange(0, len(invalidRoles)-1).Draw(t, "invalidRoleIdx")
			roles = append(roles, invalidRoles[idx])
		}

		perm := genPermission(t)

		if HasPermission(roles, perm) {
			t.Fatalf("HasPermission(%v, %q) = true, but no valid roles were provided",
				roles, perm)
		}
	})
}
