package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database with the required tables.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&domain.User{}, &domain.UserRole{})
	require.NoError(t, err)

	return db
}

// setupUserTestRouter creates a Gin test router with the UserHandler registered.
// The user parameter is set in context to simulate an authenticated admin user.
func setupUserTestRouter(t *testing.T, db *gorm.DB, user *UserInfo) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	logger := zap.NewNop()
	handler := NewUserHandler(db, logger)

	router := gin.New()
	api := router.Group("/api/v1")
	// Simulate auth middleware setting user context
	api.Use(func(c *gin.Context) {
		if user != nil {
			c.Set("user", user)
		}
		c.Next()
	})
	handler.RegisterRoutes(api)

	return router
}

// seedUsers creates test users in the database and returns them.
func seedUsers(t *testing.T, db *gorm.DB) []domain.User {
	t.Helper()
	now := time.Now()

	users := []domain.User{
		{
			ExternalID:  "ext-1",
			Email:       "admin@example.com",
			DisplayName: "Admin User",
			LastLoginAt: &now,
		},
		{
			ExternalID:  "ext-2",
			Email:       "operator@example.com",
			DisplayName: "Operator User",
			LastLoginAt: &now,
		},
		{
			ExternalID:  "ext-3",
			Email:       "viewer@example.com",
			DisplayName: "Viewer User",
		},
	}

	for i := range users {
		require.NoError(t, db.Create(&users[i]).Error)
	}

	// Assign roles
	roles := []domain.UserRole{
		{UserID: users[0].ID, Role: "admin"},
		{UserID: users[1].ID, Role: "operator"},
		{UserID: users[2].ID, Role: "viewer"},
	}
	for i := range roles {
		require.NoError(t, db.Create(&roles[i]).Error)
	}

	return users
}

func TestNewUserHandler(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	handler := NewUserHandler(db, logger)

	require.NotNil(t, handler)
	assert.Equal(t, db, handler.DB)
	assert.Equal(t, logger, handler.Logger)
}

// --- HandleListUsers Tests ---

func TestHandleListUsers_Success(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(3), resp.Total)
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 20, resp.PageSize)
	assert.Equal(t, 1, resp.TotalPages)
	assert.Len(t, resp.Users, 3)

	// Verify users have expected fields populated
	found := false
	for _, u := range resp.Users {
		if u.Email == "admin@example.com" {
			found = true
			assert.Equal(t, "Admin User", u.DisplayName)
			assert.Equal(t, users[0].ExternalID, u.ExternalID)
			assert.Contains(t, u.Roles, "admin")
			assert.NotNil(t, u.LastLoginAt)
		}
	}
	assert.True(t, found, "admin user should be in the response")
}

func TestHandleListUsers_Pagination(t *testing.T) {
	db := setupTestDB(t)
	seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	// Request page 1, page_size 2
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?page=1&page_size=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(3), resp.Total)
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 2, resp.PageSize)
	assert.Equal(t, 2, resp.TotalPages)
	assert.Len(t, resp.Users, 2)

	// Request page 2
	req = httptest.NewRequest(http.MethodGet, "/api/v1/users?page=2&page_size=2", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Users, 1)
}

func TestHandleListUsers_DefaultPagination(t *testing.T) {
	db := setupTestDB(t)
	seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	// No pagination params - should use defaults
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 20, resp.PageSize)
}

func TestHandleListUsers_EmptyDatabase(t *testing.T) {
	db := setupTestDB(t)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(0), resp.Total)
	assert.Len(t, resp.Users, 0)
}

func TestHandleListUsers_Forbidden_NonAdmin(t *testing.T) {
	db := setupTestDB(t)

	viewerUser := &UserInfo{ID: 1, Roles: []string{RoleViewer}}
	router := setupUserTestRouter(t, db, viewerUser)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleListUsers_Forbidden_Operator(t *testing.T) {
	db := setupTestDB(t)

	operatorUser := &UserInfo{ID: 1, Roles: []string{RoleOperator}}
	router := setupUserTestRouter(t, db, operatorUser)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleListUsers_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)
	router := setupUserTestRouter(t, db, nil) // no user

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleListUsers_InvalidPaginationDefaults(t *testing.T) {
	db := setupTestDB(t)
	seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	// Invalid page and page_size should default to safe values
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?page=-1&page_size=0", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 20, resp.PageSize)
}

func TestHandleListUsers_PageSizeCap(t *testing.T) {
	db := setupTestDB(t)
	seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	// page_size > 100 should be capped
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users?page_size=500", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 100, resp.PageSize)
}

// --- HandleUpdateRoles Tests ---

func TestHandleUpdateRoles_Success(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	// Update viewer to operator + admin
	body := updateRolesRequest{Roles: []string{"operator", "admin"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[2].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "roles updated successfully", resp["message"])

	// Verify roles in response
	userResp := resp["user"].(map[string]interface{})
	roles := userResp["roles"].([]interface{})
	assert.Len(t, roles, 2)
	roleStrs := make([]string, len(roles))
	for i, r := range roles {
		roleStrs[i] = r.(string)
	}
	assert.ElementsMatch(t, []string{"operator", "admin"}, roleStrs)

	// Verify in database
	var dbRoles []domain.UserRole
	db.Where("user_id = ?", users[2].ID).Find(&dbRoles)
	assert.Len(t, dbRoles, 2)
}

func TestHandleUpdateRoles_SingleRole(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	body := updateRolesRequest{Roles: []string{"viewer"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify admin user now only has viewer role
	var dbRoles []domain.UserRole
	db.Where("user_id = ?", users[0].ID).Find(&dbRoles)
	assert.Len(t, dbRoles, 1)
	assert.Equal(t, "viewer", dbRoles[0].Role)
}

func TestHandleUpdateRoles_InvalidRole(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	body := updateRolesRequest{Roles: []string{"superadmin"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "BAD_REQUEST", resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "invalid role")
}

func TestHandleUpdateRoles_EmptyRoles(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	body := updateRolesRequest{Roles: []string{}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateRoles_NoBody(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateRoles_UserNotFound(t *testing.T) {
	db := setupTestDB(t)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	body := updateRolesRequest{Roles: []string{"admin"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/99999/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "NOT_FOUND", resp.Error.Code)
}

func TestHandleUpdateRoles_InvalidUserID(t *testing.T) {
	db := setupTestDB(t)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	body := updateRolesRequest{Roles: []string{"admin"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/abc/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateRoles_DuplicateRoles(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	adminUser := &UserInfo{ID: 1, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	// Send duplicate roles - should deduplicate
	body := updateRolesRequest{Roles: []string{"admin", "admin", "operator"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify only 2 unique roles in database
	var dbRoles []domain.UserRole
	db.Where("user_id = ?", users[0].ID).Find(&dbRoles)
	assert.Len(t, dbRoles, 2)
}

func TestHandleUpdateRoles_Forbidden_NonAdmin(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)

	viewerUser := &UserInfo{ID: 2, Roles: []string{RoleViewer}}
	router := setupUserTestRouter(t, db, viewerUser)

	body := updateRolesRequest{Roles: []string{"admin"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleUpdateRoles_Unauthenticated(t *testing.T) {
	db := setupTestDB(t)
	users := seedUsers(t, db)
	router := setupUserTestRouter(t, db, nil)

	body := updateRolesRequest{Roles: []string{"admin"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+itoa(users[0].ID)+"/roles", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleListUsers_UserWithNoRoles(t *testing.T) {
	db := setupTestDB(t)

	// Create a user without any roles
	user := domain.User{
		ExternalID:  "ext-no-roles",
		Email:       "noroles@example.com",
		DisplayName: "No Roles User",
	}
	require.NoError(t, db.Create(&user).Error)

	adminUser := &UserInfo{ID: 99, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Users, 1)
	assert.Len(t, resp.Users[0].Roles, 0)
}

func TestHandleListUsers_UserWithNilLastLogin(t *testing.T) {
	db := setupTestDB(t)

	user := domain.User{
		ExternalID:  "ext-nil-login",
		Email:       "nillogin@example.com",
		DisplayName: "Nil Login",
	}
	require.NoError(t, db.Create(&user).Error)

	adminUser := &UserInfo{ID: 99, Roles: []string{RoleAdmin}}
	router := setupUserTestRouter(t, db, adminUser)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp userListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Users, 1)
	assert.Nil(t, resp.Users[0].LastLoginAt)
}

// itoa is a helper to convert uint to string for URL construction.
func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
