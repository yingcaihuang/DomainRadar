package config

import (
	"net/http"
	"strconv"

	"domainradar/internal/auth"
	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// EmailRulesHandler manages email alert rule configuration.
type EmailRulesHandler struct {
	db *gorm.DB
}

// NewEmailRulesHandler creates a new EmailRulesHandler.
func NewEmailRulesHandler(db *gorm.DB) *EmailRulesHandler {
	return &EmailRulesHandler{db: db}
}

// RegisterRoutes registers email alert rules endpoints.
func (h *EmailRulesHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rules := rg.Group("/config/email-alert-rules")
	{
		rules.GET("", h.ListRules)
		rules.POST("", auth.RequirePermission(auth.ConfigureIntegrations), h.CreateRule)
		rules.PUT("/:id", auth.RequirePermission(auth.ConfigureIntegrations), h.UpdateRule)
		rules.DELETE("/:id", auth.RequirePermission(auth.ConfigureIntegrations), h.DeleteRule)
		rules.POST("/reset-defaults", auth.RequirePermission(auth.ConfigureIntegrations), h.ResetDefaults)
	}
}

// ListRules returns all email alert rules.
func (h *EmailRulesHandler) ListRules(c *gin.Context) {
	var rules []domain.EmailAlertRule
	h.db.Order("rule_type ASC, threshold DESC").Find(&rules)

	if len(rules) == 0 {
		rules = h.seedDefaults()
	}

	c.JSON(http.StatusOK, gin.H{"data": rules})
}

// CreateRule creates a new email alert rule.
func (h *EmailRulesHandler) CreateRule(c *gin.Context) {
	var rule domain.EmailAlertRule
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

// UpdateRule updates an existing email alert rule.
func (h *EmailRulesHandler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule ID"))
		return
	}

	var rule domain.EmailAlertRule
	if err := h.db.First(&rule, id).Error; err != nil {
		errors.ErrorResponse(c, errors.NotFound("rule not found"))
		return
	}

	var update domain.EmailAlertRule
	if err := c.ShouldBindJSON(&update); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule data"))
		return
	}

	h.db.Model(&rule).Updates(map[string]interface{}{
		"rule_type":   update.RuleType,
		"threshold":   update.Threshold,
		"severity":    update.Severity,
		"enabled":     update.Enabled,
		"description": update.Description,
	})

	h.db.First(&rule, id)
	c.JSON(http.StatusOK, gin.H{"data": rule})
}

// DeleteRule removes a rule.
func (h *EmailRulesHandler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid rule ID"))
		return
	}
	if err := h.db.Delete(&domain.EmailAlertRule{}, id).Error; err != nil {
		errors.ErrorResponse(c, errors.NotFound("rule not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rule deleted"})
}

// ResetDefaults clears all email alert rules and re-seeds with defaults.
func (h *EmailRulesHandler) ResetDefaults(c *gin.Context) {
	h.db.Where("1=1").Delete(&domain.EmailAlertRule{})
	rules := h.seedDefaults()
	c.JSON(http.StatusOK, gin.H{"data": rules, "message": "rules reset to defaults"})
}

// seedDefaults inserts the default email alert rules.
func (h *EmailRulesHandler) seedDefaults() []domain.EmailAlertRule {
	defaults := []domain.EmailAlertRule{
		{RuleType: "total_score", Threshold: 50, Severity: "critical", Enabled: true, Description: "邮件安全总分低于 50 分"},
		{RuleType: "total_score", Threshold: 70, Severity: "warning", Enabled: true, Description: "邮件安全总分低于 70 分"},
		{RuleType: "score_drop", Threshold: 20, Severity: "warning", Enabled: true, Description: "单次评分下降超过 20 分"},
		{RuleType: "mx_score", Threshold: 0, Severity: "critical", Enabled: true, Description: "MX 记录检测为 0 分（邮件完全不可用）"},
		{RuleType: "spf_score", Threshold: 0, Severity: "critical", Enabled: true, Description: "SPF 记录缺失"},
		{RuleType: "dkim_score", Threshold: 0, Severity: "warning", Enabled: true, Description: "DKIM 记录缺失"},
		{RuleType: "dmarc_score", Threshold: 0, Severity: "warning", Enabled: true, Description: "DMARC 记录缺失"},
	}
	for i := range defaults {
		h.db.Create(&defaults[i])
	}
	return defaults
}
