package emailcheck

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"domainradar/internal/auth"
	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// EmailHandler implements the email monitoring API endpoints.
type EmailHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewEmailHandler creates a new EmailHandler.
func NewEmailHandler(db *gorm.DB, logger *zap.Logger) *EmailHandler {
	return &EmailHandler{db: db, logger: logger}
}

// RegisterRoutes registers email monitoring routes on the given router group.
func (h *EmailHandler) RegisterRoutes(group *gin.RouterGroup) {
	domains := group.Group("/domains")
	{
		domains.GET("/:id/email-monitor", auth.RequirePermission(auth.ViewDomains), h.GetMonitor)
		domains.POST("/:id/email-monitor", auth.RequirePermission(auth.ManageDomains), h.ConfigureMonitor)
		domains.POST("/:id/email-monitor/check", auth.RequirePermission(auth.ManageDomains), h.TriggerCheck)
		domains.GET("/:id/email-monitor/history", auth.RequirePermission(auth.ViewDomains), h.GetHistory)
	}
}

// --- Request/Response types ---

type configureRequest struct {
	DKIMSelectors string `json:"dkim_selectors"`
	MailServerIPs string `json:"mail_server_ips"`
}

type monitorResponse struct {
	ID            uint               `json:"id"`
	DomainID      uint               `json:"domain_id"`
	Enabled       bool               `json:"enabled"`
	DKIMSelectors string             `json:"dkim_selectors"`
	MailServerIPs string             `json:"mail_server_ips"`
	LastCheckedAt *time.Time         `json:"last_checked_at"`
	NextCheckAt   *time.Time         `json:"next_check_at"`
	LatestResult  *resultResponse    `json:"latest_result,omitempty"`
}

type resultResponse struct {
	ID          uint              `json:"id"`
	TotalScore  int               `json:"total_score"`
	Grade       string            `json:"grade"`
	MXScore     int               `json:"mx_score"`
	SPFScore    int               `json:"spf_score"`
	DKIMScore   int               `json:"dkim_score"`
	DMARCScore  int               `json:"dmarc_score"`
	PTRScore    int               `json:"ptr_score"`
	MTASTSScore int               `json:"mta_sts_score"`
	TLSRPTScore int               `json:"tlsrpt_score"`
	BIMIScore   int               `json:"bimi_score"`
	Details     *EmailCheckDetails `json:"details,omitempty"`
	CheckedAt   time.Time         `json:"checked_at"`
}

// --- Handlers ---

// GetMonitor returns the email monitor config and latest result for a domain.
func (h *EmailHandler) GetMonitor(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var monitor domain.EmailMonitor
	if err := h.db.Where("domain_id = ?", domainID).First(&monitor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Return empty config with no result
			c.JSON(http.StatusOK, gin.H{"data": monitorResponse{DomainID: uint(domainID), Enabled: false}})
			return
		}
		h.logger.Error("failed to get email monitor", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to get email monitor"))
		return
	}

	resp := monitorResponse{
		ID:            monitor.ID,
		DomainID:      monitor.DomainID,
		Enabled:       monitor.Enabled,
		DKIMSelectors: monitor.DKIMSelectors,
		MailServerIPs: monitor.MailServerIPs,
		LastCheckedAt: monitor.LastCheckedAt,
		NextCheckAt:   monitor.NextCheckAt,
	}

	// Get the latest result
	var latest domain.EmailCheckResult
	if err := h.db.Where("monitor_id = ?", monitor.ID).Order("checked_at DESC").First(&latest).Error; err == nil {
		resp.LatestResult = toResultResponse(&latest)
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// ConfigureMonitor creates or updates the email monitor config for a domain.
func (h *EmailHandler) ConfigureMonitor(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var req configureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body"))
		return
	}

	// Verify domain exists
	var d domain.NormalizedDomain
	if err := h.db.First(&d, domainID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("domain not found"))
			return
		}
		errors.ErrorResponse(c, errors.InternalServer("database error"))
		return
	}

	// Upsert monitor config
	var monitor domain.EmailMonitor
	result := h.db.Where("domain_id = ?", domainID).First(&monitor)
	if result.Error == gorm.ErrRecordNotFound {
		monitor = domain.EmailMonitor{
			DomainID:      uint(domainID),
			Enabled:       true,
			DKIMSelectors: req.DKIMSelectors,
			MailServerIPs: req.MailServerIPs,
		}
		if err := h.db.Create(&monitor).Error; err != nil {
			h.logger.Error("failed to create email monitor", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to create email monitor"))
			return
		}
	} else if result.Error != nil {
		h.logger.Error("failed to query email monitor", zap.Error(result.Error))
		errors.ErrorResponse(c, errors.InternalServer("database error"))
		return
	} else {
		updates := map[string]interface{}{
			"dkim_selectors":  req.DKIMSelectors,
			"mail_server_ips": req.MailServerIPs,
			"enabled":         true,
		}
		if err := h.db.Model(&monitor).Updates(updates).Error; err != nil {
			h.logger.Error("failed to update email monitor", zap.Error(err))
			errors.ErrorResponse(c, errors.InternalServer("failed to update email monitor"))
			return
		}
		// Refresh
		h.db.First(&monitor, monitor.ID)
	}

	c.JSON(http.StatusOK, gin.H{"data": monitorResponse{
		ID:            monitor.ID,
		DomainID:      monitor.DomainID,
		Enabled:       monitor.Enabled,
		DKIMSelectors: monitor.DKIMSelectors,
		MailServerIPs: monitor.MailServerIPs,
		LastCheckedAt: monitor.LastCheckedAt,
		NextCheckAt:   monitor.NextCheckAt,
	}})
}

// TriggerCheck performs an immediate email check for a domain.
func (h *EmailHandler) TriggerCheck(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	// Get domain
	var d domain.NormalizedDomain
	if err := h.db.First(&d, domainID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("domain not found"))
			return
		}
		errors.ErrorResponse(c, errors.InternalServer("database error"))
		return
	}

	// Get or create monitor
	var monitor domain.EmailMonitor
	if err := h.db.Where("domain_id = ?", domainID).First(&monitor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create a default monitor
			monitor = domain.EmailMonitor{
				DomainID: uint(domainID),
				Enabled:  true,
			}
			h.db.Create(&monitor)
		} else {
			errors.ErrorResponse(c, errors.InternalServer("database error"))
			return
		}
	}

	// Build config
	config := EmailCheckConfig{}
	if monitor.DKIMSelectors != "" {
		config.DKIMSelectors = splitAndTrim(monitor.DKIMSelectors)
	}
	if monitor.MailServerIPs != "" {
		config.MailServerIPs = splitAndTrim(monitor.MailServerIPs)
	}

	// Run the check
	report := RunEmailCheck(d.DomainName, config)

	// Store result
	detailsJSON, _ := json.Marshal(report.Details)
	checkResult := domain.EmailCheckResult{
		DomainID:    uint(domainID),
		MonitorID:   monitor.ID,
		TotalScore:  report.TotalScore,
		Grade:       report.Grade,
		MXScore:     report.MXScore,
		SPFScore:    report.SPFScore,
		DKIMScore:   report.DKIMScore,
		DMARCScore:  report.DMARCScore,
		PTRScore:    report.PTRScore,
		MTASTSScore: report.MTASTSScore,
		TLSRPTScore: report.TLSRPTScore,
		BIMIScore:   report.BIMIScore,
		Details:     string(detailsJSON),
		CheckedAt:   time.Now(),
	}

	if err := h.db.Create(&checkResult).Error; err != nil {
		h.logger.Error("failed to save email check result", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to save check result"))
		return
	}

	// Update monitor timestamps
	now := time.Now()
	nextCheck := now.Add(30 * time.Minute)
	h.db.Model(&monitor).Updates(map[string]interface{}{
		"last_checked_at": now,
		"next_check_at":   nextCheck,
	})

	// Cleanup old results (keep last 10)
	h.cleanupOldResults(monitor.ID)

	c.JSON(http.StatusOK, gin.H{"data": toResultResponse(&checkResult)})
}

// GetHistory returns the last 10 email check results for a domain.
func (h *EmailHandler) GetHistory(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var results []domain.EmailCheckResult
	if err := h.db.Where("domain_id = ?", domainID).
		Order("checked_at DESC").
		Limit(10).
		Find(&results).Error; err != nil {
		h.logger.Error("failed to get email check history", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to get history"))
		return
	}

	responses := make([]*resultResponse, 0, len(results))
	for i := range results {
		responses = append(responses, toResultResponse(&results[i]))
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

// --- Helpers ---

func (h *EmailHandler) cleanupOldResults(monitorID uint) {
	// Keep only the last 10 results per monitor
	var count int64
	h.db.Model(&domain.EmailCheckResult{}).Where("monitor_id = ?", monitorID).Count(&count)
	if count > 10 {
		var oldest domain.EmailCheckResult
		h.db.Where("monitor_id = ?", monitorID).
			Order("checked_at ASC").
			Offset(0).
			Limit(1).
			First(&oldest)

		h.db.Where("monitor_id = ? AND checked_at <= ?", monitorID, oldest.CheckedAt).
			Delete(&domain.EmailCheckResult{})
	}
}

func toResultResponse(result *domain.EmailCheckResult) *resultResponse {
	resp := &resultResponse{
		ID:          result.ID,
		TotalScore:  result.TotalScore,
		Grade:       result.Grade,
		MXScore:     result.MXScore,
		SPFScore:    result.SPFScore,
		DKIMScore:   result.DKIMScore,
		DMARCScore:  result.DMARCScore,
		PTRScore:    result.PTRScore,
		MTASTSScore: result.MTASTSScore,
		TLSRPTScore: result.TLSRPTScore,
		BIMIScore:   result.BIMIScore,
		CheckedAt:   result.CheckedAt,
	}

	if result.Details != "" {
		var details EmailCheckDetails
		if err := json.Unmarshal([]byte(result.Details), &details); err == nil {
			resp.Details = &details
		}
	}

	return resp
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
