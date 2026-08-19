package config

import (
	"net/http"
	"strconv"

	"domainradar/internal/auth"
	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RulesHandler manages expiration rule configuration.
type RulesHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRulesHandler creates a new RulesHandler.
func NewRulesHandler(db *gorm.DB, logger *zap.Logger) *RulesHandler {
	return &RulesHandler{db: db, logger: logger}
}

// RegisterRoutes registers expiration rules endpoints.
func (h *RulesHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rules := rg.Group("/config/expiration-rules")
	{
		rules.GET("", h.ListRules)
		rules.POST("", auth.RequirePermission(auth.ConfigureIntegrations), h.CreateRule)
		rules.PUT("/:id", auth.RequirePermission(auth.ConfigureIntegrations), h.UpdateRule)
		rules.DELETE("/:id", auth.RequirePermission(auth.ConfigureIntegrations), h.DeleteRule)
		rules.POST("/reset-defaults", auth.RequirePermission(auth.ConfigureIntegrations), h.ResetDefaults)
	}
}

// ListRules returns all expiration rules sorted by days_min.
func (h *RulesHandler) ListRules(c *gin.Context) {
	var rules []domain.ExpirationRule
	h.db.Order("days_min ASC").Find(&rules)

	// If no rules exist, seed defaults
	if len(rules) == 0 {
		rules = h.seedDefaults()
	}

	c.JSON(http.StatusOK, gin.H{"data": rules})
}

// CreateRule creates a new expiration rule.
func (h *RulesHandler) CreateRule(c *gin.Context) {
	var rule domain.ExpirationRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule data"))
		return
	}
	if err := h.db.Create(&rule).Error; err != nil {
		errors.ErrorResponse(c, errors.InternalServer("failed to create rule"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": rule})
}

// UpdateRule updates an existing rule.
func (h *RulesHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule ID"))
		return
	}

	var rule domain.ExpirationRule
	if err := h.db.First(&rule, id).Error; err != nil {
		errors.ErrorResponse(c, errors.NotFound("rule not found"))
		return
	}

	var update domain.ExpirationRule
	if err := c.ShouldBindJSON(&update); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule data"))
		return
	}

	h.db.Model(&rule).Updates(map[string]interface{}{
		"days_min":   update.DaysMin,
		"days_max":   update.DaysMax,
		"severity":   update.Severity,
		"color":      update.Color,
		"label":      update.Label,
		"score":      update.Score,
		"sort_order": update.SortOrder,
	})

	h.db.First(&rule, id)
	c.JSON(http.StatusOK, gin.H{"data": rule})
}

// DeleteRule removes a rule.
func (h *RulesHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule ID"))
		return
	}
	if err := h.db.Delete(&domain.ExpirationRule{}, id).Error; err != nil {
		errors.ErrorResponse(c, errors.NotFound("rule not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rule deleted"})
}

// ResetDefaults clears all rules and re-seeds with defaults.
func (h *RulesHandler) ResetDefaults(c *gin.Context) {
	h.db.Where("1=1").Delete(&domain.ExpirationRule{})
	rules := h.seedDefaults()
	c.JSON(http.StatusOK, gin.H{"data": rules, "message": "rules reset to defaults"})
}

// seedDefaults inserts the default expiration rules.
func (h *RulesHandler) seedDefaults() []domain.ExpirationRule {
	defaults := []domain.ExpirationRule{
		{DaysMin: -99999, DaysMax: 0, Severity: "critical", Color: "#ef4444", Label: "已过期", Score: 0, SortOrder: 1},
		{DaysMin: 0, DaysMax: 7, Severity: "critical", Color: "#ef4444", Label: "紧急", Score: 20, SortOrder: 2},
		{DaysMin: 7, DaysMax: 30, Severity: "warning", Color: "#f59e0b", Label: "即将到期", Score: 50, SortOrder: 3},
		{DaysMin: 30, DaysMax: 90, Severity: "info", Color: "#6366f1", Label: "注意", Score: 75, SortOrder: 4},
		{DaysMin: 90, DaysMax: 99999, Severity: "ok", Color: "#10b981", Label: "正常", Score: 100, SortOrder: 5},
	}
	for i := range defaults {
		h.db.Create(&defaults[i])
	}
	return defaults
}
