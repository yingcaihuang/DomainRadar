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
	// Legacy monitoring routes
	monitoring := rg.Group("/monitoring")
	{
		monitoring.GET("/websites/:domainId", h.GetWebsiteStatus)
		monitoring.GET("/uptime/:domainId", h.GetUptime)
		monitoring.GET("/certificates/:domainId", h.GetCertificateStatus)
		monitoring.GET("/email/:domainId", h.GetEmailStatus)
	}

	// New service monitor routes
	rg.GET("/domains/:id/monitors", h.ListMonitors)
	rg.POST("/domains/:id/monitors", h.AddMonitor)
	rg.DELETE("/monitors/:monitorId", h.DeleteMonitor)
	rg.POST("/monitors/:monitorId/check", h.CheckNow)
	rg.GET("/monitors/:monitorId/stats", h.GetStats)
	rg.GET("/monitors/:monitorId/checks", h.GetChecks)
}

// ---------- Service Monitor endpoints ----------

// ListMonitors returns all service monitors for a domain.
func (h *MonitorHandler) ListMonitors(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain ID"})
		return
	}

	var monitors []domain.ServiceMonitor
	if err := h.db.WithContext(c.Request.Context()).
		Where("domain_id = ?", domainID).
		Order("created_at ASC").
		Find(&monitors).Error; err != nil {
		h.logger.Error("failed to list monitors", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list monitors"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": monitors})
}

// AddMonitorRequest is the request body for creating a new service monitor.
type AddMonitorRequest struct {
	MonitorType    string `json:"monitor_type" binding:"required,oneof=tcp udp http https"`
	Target         string `json:"target" binding:"required"`
	Label          string `json:"label"`
	IntervalSec    int    `json:"interval_sec"`
	TimeoutSec     int    `json:"timeout_sec"`
	ExpectedStatus int    `json:"expected_status"`
}

// AddMonitor creates a new service monitor for a domain.
func (h *MonitorHandler) AddMonitor(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain ID"})
		return
	}

	var req AddMonitorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set defaults
	intervalSec := req.IntervalSec
	if intervalSec <= 0 {
		intervalSec = 300
	}
	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	expectedStatus := req.ExpectedStatus
	if expectedStatus <= 0 && (req.MonitorType == "http" || req.MonitorType == "https") {
		expectedStatus = 200
	}

	monitor := domain.ServiceMonitor{
		DomainID:       uint(domainID),
		MonitorType:    req.MonitorType,
		Target:         req.Target,
		Label:          req.Label,
		IntervalSec:    intervalSec,
		TimeoutSec:     timeoutSec,
		ExpectedStatus: expectedStatus,
		Enabled:        true,
	}

	if err := h.db.WithContext(c.Request.Context()).Create(&monitor).Error; err != nil {
		h.logger.Error("failed to create monitor", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create monitor"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": monitor})
}

// DeleteMonitor removes a service monitor.
func (h *MonitorHandler) DeleteMonitor(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid monitor ID"})
		return
	}

	// Delete associated checks first
	if err := h.db.WithContext(c.Request.Context()).
		Where("monitor_id = ?", monitorID).
		Delete(&domain.ServiceCheck{}).Error; err != nil {
		h.logger.Error("failed to delete checks", zap.Error(err))
	}

	if err := h.db.WithContext(c.Request.Context()).
		Delete(&domain.ServiceMonitor{}, monitorID).Error; err != nil {
		h.logger.Error("failed to delete monitor", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete monitor"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// CheckNow triggers an immediate check for a monitor and returns the result.
func (h *MonitorHandler) CheckNow(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid monitor ID"})
		return
	}

	var monitor domain.ServiceMonitor
	if err := h.db.WithContext(c.Request.Context()).First(&monitor, monitorID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "monitor not found"})
		return
	}

	check := RunProbe(&monitor)
	if err := h.db.WithContext(c.Request.Context()).Create(&check).Error; err != nil {
		h.logger.Error("failed to save check result", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save check"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": check})
}


// MonitorStats represents aggregated statistics for a monitor.
type MonitorStats struct {
	MonitorID      uint    `json:"monitor_id"`
	TotalChecks    int     `json:"total_checks"`
	SuccessChecks  int     `json:"success_checks"`
	FailedChecks   int     `json:"failed_checks"`
	UptimePercent  float64 `json:"uptime_percent"`
	AvgResponseMs  float64 `json:"avg_response_ms"`
	MaxResponseMs  int64   `json:"max_response_ms"`
	MinResponseMs  int64   `json:"min_response_ms"`
}

// GetStats returns aggregated stats for a monitor over the last 7 days.
func (h *MonitorHandler) GetStats(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid monitor ID"})
		return
	}

	since := time.Now().Add(-7 * 24 * time.Hour)

	var checks []domain.ServiceCheck
	if err := h.db.WithContext(c.Request.Context()).
		Where("monitor_id = ? AND checked_at >= ?", monitorID, since).
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to query checks for stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query stats"})
		return
	}

	stats := MonitorStats{
		MonitorID: uint(monitorID),
	}

	if len(checks) == 0 {
		stats.UptimePercent = 100.0
		c.JSON(http.StatusOK, gin.H{"data": stats})
		return
	}

	var totalResponseMs int64
	var maxResponseMs int64
	minResponseMs := int64(math.MaxInt64)

	for _, ch := range checks {
		stats.TotalChecks++
		if ch.Success {
			stats.SuccessChecks++
		}
		totalResponseMs += ch.ResponseTimeMs
		if ch.ResponseTimeMs > maxResponseMs {
			maxResponseMs = ch.ResponseTimeMs
		}
		if ch.ResponseTimeMs < minResponseMs && ch.ResponseTimeMs > 0 {
			minResponseMs = ch.ResponseTimeMs
		}
	}

	stats.FailedChecks = stats.TotalChecks - stats.SuccessChecks
	if stats.TotalChecks > 0 {
		stats.UptimePercent = math.Round(float64(stats.SuccessChecks)/float64(stats.TotalChecks)*10000) / 100
		stats.AvgResponseMs = math.Round(float64(totalResponseMs)/float64(stats.TotalChecks)*100) / 100
	}
	stats.MaxResponseMs = maxResponseMs
	if minResponseMs == int64(math.MaxInt64) {
		minResponseMs = 0
	}
	stats.MinResponseMs = minResponseMs

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// GetChecks returns the last 50 check records for a monitor (for trend chart).
func (h *MonitorHandler) GetChecks(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid monitor ID"})
		return
	}

	var checks []domain.ServiceCheck
	if err := h.db.WithContext(c.Request.Context()).
		Where("monitor_id = ?", monitorID).
		Order("checked_at DESC").
		Limit(50).
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to query checks", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query checks"})
		return
	}

	// Reverse to chronological order for charts
	for i, j := 0, len(checks)-1; i < j; i, j = i+1, j-1 {
		checks[i], checks[j] = checks[j], checks[i]
	}

	c.JSON(http.StatusOK, gin.H{"data": checks})
}

// ---------- Legacy monitoring endpoints (kept for backward compatibility) ----------

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
