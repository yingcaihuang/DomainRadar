package auth

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Session Manager Tests ---

func TestSessionManager_CreateAndGet(t *testing.T) {
	sm := NewSessionManager(1 * time.Hour)

	session, err := sm.CreateSession(1, "user@example.com", "Test User", []string{"admin"})
	require.NoError(t, err)
	assert.NotEmpty(t, session.Token)
	assert.Equal(t, uint(1), session.UserID)
	assert.Equal(t, "user@example.com", session.Email)
	assert.Equal(t, "Test User", session.Name)
	assert.Equal(t, []string{"admin"}, session.Roles)

	// Retrieve the session
	retrieved := sm.GetSession(session.Token)
	require.NotNil(t, retrieved)
	assert.Equal(t, session.Token, retrieved.Token)
	assert.Equal(t, session.UserID, retrieved.UserID)
}

func TestSessionManager_GetNonExistent(t *testing.T) {
	sm := NewSessionManager(1 * time.Hour)

	session := sm.GetSession("nonexistent-token")
	assert.Nil(t, session)
}

func TestSessionManager_DeleteSession(t *testing.T) {
	sm := NewSessionManager(1 * time.Hour)

	session, err := sm.CreateSession(1, "user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)

	// Delete and verify it's gone
	sm.DeleteSession(session.Token)
	assert.Nil(t, sm.GetSession(session.Token))
}

func TestSessionManager_ExpiredSession(t *testing.T) {
	// Create a session manager with very short TTL
	sm := NewSessionManager(1 * time.Millisecond)

	session, err := sm.CreateSession(1, "user@example.com", "User", []string{"viewer"})
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(5 * time.Millisecond)

	// Session should be expired
	retrieved := sm.GetSession(session.Token)
	assert.Nil(t, retrieved)
}

func TestSessionManager_InvalidateUserSessions(t *testing.T) {
	sm := NewSessionManager(1 * time.Hour)

	// Create multiple sessions for the same user
	s1, err := sm.CreateSession(1, "user@example.com", "User", []string{"admin"})
	require.NoError(t, err)
	s2, err := sm.CreateSession(1, "user@example.com", "User", []string{"admin"})
	require.NoError(t, err)
	// Create a session for a different user
	s3, err := sm.CreateSession(2, "other@example.com", "Other", []string{"viewer"})
	require.NoError(t, err)

	// Invalidate user 1's sessions
	sm.InvalidateUserSessions(1)

	assert.Nil(t, sm.GetSession(s1.Token))
	assert.Nil(t, sm.GetSession(s2.Token))
	// User 2's session should still be valid
	assert.NotNil(t, sm.GetSession(s3.Token))
}

func TestSessionManager_ActiveSessionCount(t *testing.T) {
	sm := NewSessionManager(1 * time.Hour)

	assert.Equal(t, 0, sm.ActiveSessionCount())

	_, _ = sm.CreateSession(1, "a@b.com", "A", []string{"admin"})
	_, _ = sm.CreateSession(2, "b@b.com", "B", []string{"viewer"})

	assert.Equal(t, 2, sm.ActiveSessionCount())
}

func TestSessionManager_UniqueTokens(t *testing.T) {
	sm := NewSessionManager(1 * time.Hour)

	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		session, err := sm.CreateSession(uint(i), "user@example.com", "User", []string{"viewer"})
		require.NoError(t, err)
		assert.False(t, tokens[session.Token], "duplicate token generated")
		tokens[session.Token] = true
	}
}

// --- Role Mapping Tests ---

func TestMapGroupsToRoles_AdminGroup(t *testing.T) {
	roles := MapGroupsToRoles([]string{"DomainRadar-Admin"})
	assert.Contains(t, roles, "admin")
}

func TestMapGroupsToRoles_OperatorGroup(t *testing.T) {
	roles := MapGroupsToRoles([]string{"DomainRadar-Operator"})
	assert.Contains(t, roles, "operator")
}

func TestMapGroupsToRoles_ViewerGroup(t *testing.T) {
	roles := MapGroupsToRoles([]string{"DomainRadar-Viewer"})
	assert.Contains(t, roles, "viewer")
}

func TestMapGroupsToRoles_MultipleGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{"Team-Admin", "App-Operator"})
	sort.Strings(roles)
	assert.Equal(t, []string{"admin", "operator"}, roles)
}

func TestMapGroupsToRoles_CaseInsensitive(t *testing.T) {
	roles := MapGroupsToRoles([]string{"ADMIN-Group"})
	assert.Contains(t, roles, "admin")

	roles = MapGroupsToRoles([]string{"operator-team"})
	assert.Contains(t, roles, "operator")
}

func TestMapGroupsToRoles_NoRecognizedGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{"engineering", "backend-team"})
	assert.Equal(t, []string{"viewer"}, roles)
}

func TestMapGroupsToRoles_EmptyGroups(t *testing.T) {
	roles := MapGroupsToRoles([]string{})
	assert.Equal(t, []string{"viewer"}, roles)
}

func TestMapGroupsToRoles_NilGroups(t *testing.T) {
	roles := MapGroupsToRoles(nil)
	assert.Equal(t, []string{"viewer"}, roles)
}

func TestMapGroupsToRoles_DeduplicatesRoles(t *testing.T) {
	// Multiple groups that all map to admin should produce only one "admin"
	roles := MapGroupsToRoles([]string{"super-admin", "cloud-admin"})
	adminCount := 0
	for _, r := range roles {
		if r == "admin" {
			adminCount++
		}
	}
	assert.Equal(t, 1, adminCount)
}

// --- Middleware Tests ---

func TestAuthMiddleware_NoToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sm := NewSessionManager(1 * time.Hour)

	router := gin.New()
	router.Use(AuthMiddleware(sm))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sm := NewSessionManager(1 * time.Hour)

	router := gin.New()
	router.Use(AuthMiddleware(sm))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-here")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ValidToken_BearerHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sm := NewSessionManager(1 * time.Hour)

	session, err := sm.CreateSession(42, "test@example.com", "Test", []string{"admin"})
	require.NoError(t, err)

	var capturedUserID uint
	var capturedRoles []string

	router := gin.New()
	router.Use(AuthMiddleware(sm))
	router.GET("/test", func(c *gin.Context) {
		capturedUserID, _ = GetUserID(c)
		capturedRoles = GetUserRoles(c)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, uint(42), capturedUserID)
	assert.Equal(t, []string{"admin"}, capturedRoles)
}

func TestAuthMiddleware_ValidToken_Cookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sm := NewSessionManager(1 * time.Hour)

	session, err := sm.CreateSession(7, "cookie@example.com", "Cookie User", []string{"operator"})
	require.NoError(t, err)

	var capturedEmail string

	router := gin.New()
	router.Use(AuthMiddleware(sm))
	router.GET("/test", func(c *gin.Context) {
		capturedEmail = GetUserEmail(c)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: session.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "cookie@example.com", capturedEmail)
}

func TestAuthMiddleware_ExpiredSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sm := NewSessionManager(1 * time.Millisecond)

	session, err := sm.CreateSession(1, "expired@example.com", "Expired", []string{"viewer"})
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)

	router := gin.New()
	router.Use(AuthMiddleware(sm))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- SessionData Tests ---

func TestSessionData_IsExpired(t *testing.T) {
	session := &SessionData{
		ExpiresAt: time.Now().Add(-1 * time.Second),
	}
	assert.True(t, session.IsExpired())

	session.ExpiresAt = time.Now().Add(1 * time.Hour)
	assert.False(t, session.IsExpired())
}
