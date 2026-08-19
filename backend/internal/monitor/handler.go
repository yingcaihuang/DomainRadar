package monitor

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"domainradar/internal/domain"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MonitorHandler provides HTTP endpoints for monitoring data.
type MonitorHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewMonitorHandler creates a new MonitorHandler.
func NewMonitorHandler(db *gorm.DB, logger *zap.Logger) *MonitorHandler {
	return &MonitorHandler{
		db:     db,
		logger: logger,
	}
}

// RegisterRoutes registers all monitoring endpoints on the provided Gin router group.
func (h *MonitorHandler) RegisterRoutes(rg *gin.RouterGroup) {
	monitoring := rg.Group("/monitoring")
	{
		monitoring.GET("/websites/:domainId", h.GetWebsiteStatus)
		monitoring.GET("/uptime/:domainId", h.GetUptime)
		monitoring.GET("/certificates/:domainId", h.GetCertificateStatus)
		monitoring.GET("/email/:domainId", h.GetEmailStatus)
	}
}

// GetWebsiteStatus returns recent health check results for a domain.
func (h *MonitorHandler) GetWebsiteStatus(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("domainId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain ID"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	var checks []domain.HealthCheck
	if err := h.db.WithContext(c.Request.Context()).
		Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		Limit(limit).
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to query health checks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query health checks"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"domain_id": domainID,
		"checks":    checks,
		"total":     len(checks),
	})
}

// UptimeResponse represents the uptime statistics for a domain.
type UptimeResponse struct {
	DomainID          uint    `json:"domain_id"`
	Period            string  `json:"period"`
	TotalChecks       int     `json:"total_checks"`
	SuccessfulChecks  int     `json:"successful_checks"`
	FailedChecks      int     `json:"failed_checks"`
	UptimePercentage  float64 `json:"uptime_percentage"`
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
	MaxResponseTimeMs int     `json:"max_response_time_ms"`
	MinResponseTimeMs int     `json:"min_response_time_ms"`
}

// GetUptime returns uptime statistics for a domain over a selectable time period.
func (h *MonitorHandler) GetUptime(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("domainId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain ID"})
		return
	}

	// Parse period: 24h, 7d, 30d (default 24h).
	period := c.DefaultQuery("period", "24h")
	var since time.Time
	now := time.Now()
	switch period {
	case "7d":
		since = now.Add(-7 * 24 * time.Hour)
	case "30d":
		since = now.Add(-30 * 24 * time.Hour)
	default:
		period = "24h"
		since = now.Add(-24 * time.Hour)
	}

	var checks []domain.HealthCheck
	if err := h.db.WithContext(c.Request.Context()).
		Where("domain_id = ? AND checked_at >= ?", domainID, since).
		Order("checked_at ASC").
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to query health checks for uptime", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query uptime data"})
		return
	}

	// Get domain's response time threshold.
	var d domain.NormalizedDomain
	h.db.WithContext(c.Request.Context()).Select("response_time_threshold_ms").First(&d, domainID)
	threshold := d.ResponseTimeThresholdMs
	if threshold <= 0 {
		threshold = DefaultResponseTimeThresholdMs
	}

	// Calculate statistics.
	totalChecks := len(checks)
	successfulChecks := 0
	var totalResponseTime int64
	maxResponseTime := 0
	minResponseTime := math.MaxInt32

	for _, check := range checks {
		isSuccess := check.FailureCategory == "" && check.HTTPStatusCode >= 200 && check.HTTPStatusCode < 300 && check.ResponseTimeMs <= threshold
		if isSuccess {
			successfulChecks++
		}
		totalResponseTime += int64(check.ResponseTimeMs)
		if check.ResponseTimeMs > maxResponseTime {
			maxResponseTime = check.ResponseTimeMs
		}
		if check.ResponseTimeMs < minResponseTime && check.ResponseTimeMs > 0 {
			minResponseTime = check.ResponseTimeMs
		}
	}

	if minResponseTime == math.MaxInt32 {
		minResponseTime = 0
	}

	var avgResponseTime float64
	if totalChecks > 0 {
		avgResponseTime = float64(totalResponseTime) / float64(totalChecks)
	}

	response := UptimeResponse{
		DomainID:          uint(domainID),
		Period:            period,
		TotalChecks:       totalChecks,
		SuccessfulChecks:  successfulChecks,
		FailedChecks:      totalChecks - successfulChecks,
		UptimePercentage:  CalculateUptime(totalChecks, successfulChecks),
		AvgResponseTimeMs: math.Round(avgResponseTime*100) / 100,
		MaxResponseTimeMs: maxResponseTime,
		MinResponseTimeMs: minResponseTime,
	}

	c.JSON(http.StatusOK, response)
}

// GetCertificateStatus returns the latest certificate check for a domain.
func (h *MonitorHandler) GetCertificateStatus(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("domainId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain ID"})
		return
	}

	var checks []domain.CertificateCheck
	if err := h.db.WithContext(c.Request.Context()).
		Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		Limit(10).
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to query certificate checks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query certificate data"})
		return
	}

	var latestCheck *domain.CertificateCheck
	if len(checks) > 0 {
		latestCheck = &checks[0]
	}

	c.JSON(http.StatusOK, gin.H{
		"domain_id": domainID,
		"latest":    latestCheck,
		"history":   checks,
	})
}

// GetEmailStatus returns the latest email compliance check for a domain.
func (h *MonitorHandler) GetEmailStatus(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("domainId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain ID"})
		return
	}

	var checks []domain.EmailCheck
	if err := h.db.WithContext(c.Request.Context()).
		Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		Limit(10).
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to query email checks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query email data"})
		return
	}

	var latestCheck *domain.EmailCheck
	if len(checks) > 0 {
		latestCheck = &checks[0]
	}

	c.JSON(http.StatusOK, gin.H{
		"domain_id": domainID,
		"latest":    latestCheck,
		"history":   checks,
	})
}
