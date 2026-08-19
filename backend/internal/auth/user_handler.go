package auth

import (
	"net/http"
	"strconv"

	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// validRoles defines the set of roles that can be assigned to users.
var validRoles = map[string]bool{
	RoleViewer:   true,
	RoleOperator: true,
	RoleAdmin:    true,
}

// UserHandler implements user management endpoints.
type UserHandler struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewUserHandler creates a new UserHandler with the given dependencies.
func NewUserHandler(db *gorm.DB, logger *zap.Logger) *UserHandler {
	return &UserHandler{
		DB:     db,
		Logger: logger,
	}
}

// userListResponse is the response body for the list users endpoint.
type userListResponse struct {
	Users      []userResponse `json:"users"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

// userResponse represents a single user in API responses.
type userResponse struct {
	ID          uint       `json:"id"`
	ExternalID  string     `json:"external_id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Roles       []string   `json:"roles"`
	LastLoginAt *string    `json:"last_login_at"`
	CreatedAt   string     `json:"created_at"`
}

// updateRolesRequest is the request body for updating user roles.
type updateRolesRequest struct {
	Roles []string `json:"roles" binding:"required"`
}

// HandleListUsers lists all users with their roles, last login, and activity.
// GET /api/v1/users
func (h *UserHandler) HandleListUsers(c *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// Count total users
	var total int64
	if err := h.DB.Model(&domain.User{}).Count(&total).Error; err != nil {
		h.Logger.Error("Failed to count users", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve users"))
		return
	}

	// Calculate pagination
	offset := (page - 1) * pageSize
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	// Fetch users with preloaded roles
	var users []domain.User
	if err := h.DB.Preload("Roles").
		Order("created_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&users).Error; err != nil {
		h.Logger.Error("Failed to list users", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve users"))
		return
	}

	// Build response
	userResponses := make([]userResponse, 0, len(users))
	for _, u := range users {
		userResponses = append(userResponses, toUserResponse(u))
	}

	c.JSON(http.StatusOK, userListResponse{
		Users:      userResponses,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// HandleUpdateRoles updates the roles for a specific user.
// PUT /api/v1/users/:id/roles
func (h *UserHandler) HandleUpdateRoles(c *gin.Context) {
	// Parse user ID from path
	idStr := c.Param("id")
	userID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid user ID"))
		return
	}

	// Parse request body
	var req updateRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: roles field is required"))
		return
	}

	// Validate that at least one role is provided
	if len(req.Roles) == 0 {
		errors.ErrorResponse(c, errors.BadRequest("at least one role is required"))
		return
	}

	// Validate all provided roles
	for _, role := range req.Roles {
		if !validRoles[role] {
			errors.ErrorResponse(c, errors.BadRequest("invalid role: "+role+"; valid roles are: viewer, operator, admin"))
			return
		}
	}

	// Deduplicate roles
	roleSet := make(map[string]bool)
	for _, role := range req.Roles {
		roleSet[role] = true
	}

	// Verify user exists
	var user domain.User
	if err := h.DB.First(&user, uint(userID)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("user not found"))
			return
		}
		h.Logger.Error("Failed to find user", zap.Error(err), zap.Uint64("user_id", userID))
		errors.ErrorResponse(c, errors.InternalServer("failed to update user roles"))
		return
	}

	// Update roles in a transaction: delete existing, create new ones
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		// Delete existing roles
		if err := tx.Where("user_id = ?", uint(userID)).Delete(&domain.UserRole{}).Error; err != nil {
			return err
		}

		// Create new roles
		for role := range roleSet {
			userRole := domain.UserRole{
				UserID: uint(userID),
				Role:   role,
			}
			if err := tx.Create(&userRole).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		h.Logger.Error("Failed to update user roles", zap.Error(err), zap.Uint64("user_id", userID))
		errors.ErrorResponse(c, errors.InternalServer("failed to update user roles"))
		return
	}

	// Reload user with updated roles
	if err := h.DB.Preload("Roles").First(&user, uint(userID)).Error; err != nil {
		h.Logger.Error("Failed to reload user after role update", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve updated user"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "roles updated successfully",
		"user":    toUserResponse(user),
	})
}

// RegisterRoutes registers user management routes on the given router group.
// All user management endpoints require the ManageUsers permission.
func (h *UserHandler) RegisterRoutes(group *gin.RouterGroup) {
	users := group.Group("/users")
	users.Use(RequirePermission(ManageUsers))
	{
		users.GET("", h.HandleListUsers)
		users.PUT("/:id/roles", h.HandleUpdateRoles)
	}
}

// toUserResponse converts a domain.User to the API response format.
func toUserResponse(u domain.User) userResponse {
	roles := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		roles = append(roles, r.Role)
	}

	var lastLogin *string
	if u.LastLoginAt != nil {
		formatted := u.LastLoginAt.Format("2006-01-02T15:04:05Z")
		lastLogin = &formatted
	}

	return userResponse{
		ID:          u.ID,
		ExternalID:  u.ExternalID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Roles:       roles,
		LastLoginAt: lastLogin,
		CreatedAt:   u.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}
