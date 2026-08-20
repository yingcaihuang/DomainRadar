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

// GroupMappingHandler implements group-to-role mapping endpoints.
type GroupMappingHandler struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewGroupMappingHandler creates a new GroupMappingHandler.
func NewGroupMappingHandler(db *gorm.DB, logger *zap.Logger) *GroupMappingHandler {
	return &GroupMappingHandler{DB: db, Logger: logger}
}

// createGroupMappingRequest is the request body for creating a group mapping.
type createGroupMappingRequest struct {
	GroupName string `json:"group_name" binding:"required"`
	Role      string `json:"role" binding:"required"`
}

// HandleListGroupMappings lists all group-to-role mappings.
// GET /api/v1/config/group-mappings
func (h *GroupMappingHandler) HandleListGroupMappings(c *gin.Context) {
	var mappings []domain.GroupMapping
	if err := h.DB.Order("created_at DESC").Find(&mappings).Error; err != nil {
		h.Logger.Error("Failed to list group mappings", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve group mappings"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": mappings})
}

// HandleCreateGroupMapping creates a new group-to-role mapping.
// POST /api/v1/config/group-mappings
func (h *GroupMappingHandler) HandleCreateGroupMapping(c *gin.Context) {
	var req createGroupMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("group_name and role are required"))
		return
	}

	// Validate role
	if !validRoles[req.Role] {
		errors.ErrorResponse(c, errors.BadRequest("invalid role: "+req.Role+"; valid roles are: viewer, operator, admin"))
		return
	}

	mapping := domain.GroupMapping{
		GroupName: req.GroupName,
		Role:      req.Role,
	}

	if err := h.DB.Create(&mapping).Error; err != nil {
		// Check for unique constraint violation
		if err.Error() != "" {
			var existing domain.GroupMapping
			if h.DB.Where("group_name = ?", req.GroupName).First(&existing).Error == nil {
				errors.ErrorResponse(c, errors.BadRequest("mapping for this group already exists"))
				return
			}
		}
		h.Logger.Error("Failed to create group mapping", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create group mapping"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "group mapping created successfully",
		"data":    mapping,
	})
}

// HandleDeleteGroupMapping deletes a group mapping by ID.
// DELETE /api/v1/config/group-mappings/:id
func (h *GroupMappingHandler) HandleDeleteGroupMapping(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid mapping ID"))
		return
	}

	result := h.DB.Delete(&domain.GroupMapping{}, uint(id))
	if result.Error != nil {
		h.Logger.Error("Failed to delete group mapping", zap.Error(result.Error))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete group mapping"))
		return
	}
	if result.RowsAffected == 0 {
		errors.ErrorResponse(c, errors.NotFound("group mapping not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "group mapping deleted successfully"})
}

// RegisterRoutes registers group mapping routes. Requires admin role.
func (h *GroupMappingHandler) RegisterRoutes(group *gin.RouterGroup) {
	mappings := group.Group("/config/group-mappings")
	mappings.Use(RequireRole(RoleAdmin))
	{
		mappings.GET("", h.HandleListGroupMappings)
		mappings.POST("", h.HandleCreateGroupMapping)
		mappings.DELETE("/:id", h.HandleDeleteGroupMapping)
	}
}

// ResolveGroupRoles looks up explicit mappings from the GroupMapping table.
// Returns the mapped roles, or nil if no mappings were found.
func ResolveGroupRoles(db *gorm.DB, groups []string) []string {
	if len(groups) == 0 {
		return nil
	}

	var mappings []domain.GroupMapping
	if err := db.Where("group_name IN ?", groups).Find(&mappings).Error; err != nil {
		return nil
	}

	if len(mappings) == 0 {
		return nil
	}

	roleSet := make(map[string]bool)
	for _, m := range mappings {
		roleSet[m.Role] = true
	}

	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	return roles
}
