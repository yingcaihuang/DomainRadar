package auth

import (
	"net/http"
	"time"

	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// localLoginRequest is the request body for local username/password login.
type localLoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// changePasswordRequest is the request body for changing the user's password.
type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// HandleLocalLogin authenticates a local user with username (external_id or email) and password.
// POST /api/v1/auth/login
func (h *AuthHandler) HandleLocalLogin(c *gin.Context) {
	var req localLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("username and password are required"))
		return
	}

	// Find user by external_id (username) or email
	var user domain.User
	result := h.DB.Preload("Roles").
		Where("external_id = ? OR email = ?", req.Username, req.Username).
		First(&user)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.Unauthorized("invalid username or password"))
			return
		}
		h.Logger.Error("Database error during login", zap.Error(result.Error))
		errors.ErrorResponse(c, errors.InternalServer("login failed"))
		return
	}

	// Only local users can login with password
	if user.AuthSource != "local" {
		errors.ErrorResponse(c, errors.BadRequest("this account uses SSO login"))
		return
	}

	// Verify password
	if user.PasswordHash == "" {
		errors.ErrorResponse(c, errors.Unauthorized("invalid username or password"))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		errors.ErrorResponse(c, errors.Unauthorized("invalid username or password"))
		return
	}

	// Update last login time
	now := time.Now()
	user.LastLoginAt = &now
	h.DB.Save(&user)

	// Get roles
	roles := make([]string, 0, len(user.Roles))
	for _, r := range user.Roles {
		roles = append(roles, r.Role)
	}
	if len(roles) == 0 {
		roles = []string{RoleViewer}
	}

	// Create session
	session, err := h.SessionManager.CreateSession(user.ID, user.Email, user.DisplayName, roles)
	if err != nil {
		h.Logger.Error("Failed to create session", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create session"))
		return
	}

	// Set session cookie
	c.SetCookie("session_token", session.Token, int(h.SessionManager.ttl.Seconds()), "/", "", false, true)

	h.Logger.Info("Local user logged in", zap.String("username", req.Username), zap.Uint("user_id", user.ID))

	c.JSON(http.StatusOK, gin.H{
		"message": "login successful",
		"user": gin.H{
			"id":                   user.ID,
			"email":                user.Email,
			"display_name":         user.DisplayName,
			"roles":                roles,
			"must_change_password": user.MustChangePassword,
		},
		"token": session.Token,
	})
}

// HandleChangePassword allows an authenticated user to change their password.
// POST /api/v1/auth/change-password
func (h *AuthHandler) HandleChangePassword(c *gin.Context) {
	// Get current user from session
	token := extractSessionToken(c)
	if token == "" {
		errors.ErrorResponse(c, errors.Unauthorized("authentication required"))
		return
	}

	session := h.SessionManager.GetSession(token)
	if session == nil {
		errors.ErrorResponse(c, errors.Unauthorized("session expired or invalid"))
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("old_password and new_password (min 6 chars) are required"))
		return
	}

	// Find the user
	var user domain.User
	if err := h.DB.First(&user, session.UserID).Error; err != nil {
		h.Logger.Error("Failed to find user for password change", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to change password"))
		return
	}

	// Only local users can change password
	if user.AuthSource != "local" {
		errors.ErrorResponse(c, errors.BadRequest("SSO users cannot change password here"))
		return
	}

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		errors.ErrorResponse(c, errors.Unauthorized("current password is incorrect"))
		return
	}

	// Hash new password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		h.Logger.Error("Failed to hash new password", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to change password"))
		return
	}

	// Update user
	user.PasswordHash = string(hash)
	user.MustChangePassword = false
	if err := h.DB.Save(&user).Error; err != nil {
		h.Logger.Error("Failed to save new password", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to change password"))
		return
	}

	h.Logger.Info("User changed password", zap.Uint("user_id", user.ID))

	c.JSON(http.StatusOK, gin.H{
		"message": "password changed successfully",
	})
}

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
