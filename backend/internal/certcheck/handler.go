package certcheck

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"domainradar/internal/auth"
	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CertHandler implements the certificate monitoring API endpoints.
type CertHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewCertHandler creates a new CertHandler.
func NewCertHandler(db *gorm.DB, logger *zap.Logger) *CertHandler {
	return &CertHandler{db: db, logger: logger}
}

// RegisterRoutes registers certificate monitoring routes on the given router group.
func (h *CertHandler) RegisterRoutes(group *gin.RouterGroup) {
	// Domain-scoped routes
	domains := group.Group("/domains")
	{
		domains.GET("/:id/certificates", auth.RequirePermission(auth.ViewDomains), h.ListMonitors)
		domains.POST("/:id/certificates", auth.RequirePermission(auth.ManageDomains), h.AddMonitor)
	}

	// Monitor-scoped routes
	certs := group.Group("/certificates")
	{
		certs.DELETE("/:monitorId", auth.RequirePermission(auth.ManageDomains), h.DeleteMonitor)
		certs.POST("/:monitorId/check", auth.RequirePermission(auth.ManageDomains), h.CheckNow)
		certs.GET("/:monitorId/history", auth.RequirePermission(auth.ViewDomains), h.GetHistory)
	}
}

// --- Request/Response types ---

type addMonitorRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	Label    string `json:"label"`
}

type monitorResponse struct {
	ID        uint               `json:"id"`
	DomainID  uint               `json:"domain_id"`
	Endpoint  string             `json:"endpoint"`
	Label     string             `json:"label"`
	Enabled   bool               `json:"enabled"`
	CreatedAt time.Time          `json:"created_at"`
	Latest    *checkResponse     `json:"latest,omitempty"`
}

type checkResponse struct {
	ID            uint      `json:"id"`
	Subject       string    `json:"subject"`
	Issuer        string    `json:"issuer"`
	ValidFrom     time.Time `json:"valid_from"`
	ValidTo       time.Time `json:"valid_to"`
	DaysRemaining int       `json:"days_remaining"`
	SANs          []string  `json:"sans"`
	ChainComplete bool      `json:"chain_complete"`
	Error         string    `json:"error,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

// --- Handlers ---

// ListMonitors returns all certificate monitors for a domain, including their latest check.
func (h *CertHandler) ListMonitors(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var monitors []domain.CertificateMonitor
	if err := h.db.Where("domain_id = ?", domainID).Order("created_at ASC").Find(&monitors).Error; err != nil {
		h.logger.Error("failed to list monitors", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to list monitors"))
		return
	}

	responses := make([]monitorResponse, 0, len(monitors))
	for _, m := range monitors {
		resp := monitorResponse{
			ID:        m.ID,
			DomainID:  m.DomainID,
			Endpoint:  m.Endpoint,
			Label:     m.Label,
			Enabled:   m.Enabled,
			CreatedAt: m.CreatedAt,
		}

		// Get the latest check for this monitor
		var latest domain.CertificateCheck
		if err := h.db.Where("monitor_id = ?", m.ID).Order("checked_at DESC").First(&latest).Error; err == nil {
			resp.Latest = toCheckResponse(&latest)
		}

		responses = append(responses, resp)
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

// AddMonitor creates a new certificate monitor for a domain.
func (h *CertHandler) AddMonitor(c *gin.Context) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var req addMonitorRequest
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

	monitor := domain.CertificateMonitor{
		DomainID: uint(domainID),
		Endpoint: req.Endpoint,
		Label:    req.Label,
		Enabled:  true,
	}

	if err := h.db.Create(&monitor).Error; err != nil {
		h.logger.Error("failed to create monitor", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create monitor"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": monitorResponse{
		ID:        monitor.ID,
		DomainID:  monitor.DomainID,
		Endpoint:  monitor.Endpoint,
		Label:     monitor.Label,
		Enabled:   monitor.Enabled,
		CreatedAt: monitor.CreatedAt,
	}})
}

// DeleteMonitor removes a certificate monitor.
func (h *CertHandler) DeleteMonitor(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid monitor ID"))
		return
	}

	result := h.db.Delete(&domain.CertificateMonitor{}, monitorID)
	if result.Error != nil {
		h.logger.Error("failed to delete monitor", zap.Error(result.Error))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete monitor"))
		return
	}
	if result.RowsAffected == 0 {
		errors.ErrorResponse(c, errors.NotFound("monitor not found"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "monitor deleted"})
}

// CheckNow triggers an immediate certificate check for a monitor.
func (h *CertHandler) CheckNow(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid monitor ID"))
		return
	}

	var monitor domain.CertificateMonitor
	if err := h.db.First(&monitor, monitorID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("monitor not found"))
			return
		}
		errors.ErrorResponse(c, errors.InternalServer("database error"))
		return
	}

	// Perform the check
	result, checkErr := CheckEndpoint(monitor.Endpoint, 10*time.Second)
	if checkErr != nil {
		result = &CertResult{Error: checkErr.Error()}
	}

	// Store the result
	sansJSON, _ := json.Marshal(result.SANs)
	check := domain.CertificateCheck{
		DomainID:      monitor.DomainID,
		MonitorID:     monitor.ID,
		Subject:       result.Subject,
		Issuer:        result.Issuer,
		ValidFrom:     result.ValidFrom,
		ValidTo:       result.ValidTo,
		DaysRemaining: result.DaysRemaining,
		ChainComplete: result.ChainComplete,
		SANs:          string(sansJSON),
		SerialNumber:  result.SerialNumber,
		Error:         result.Error,
		CheckedAt:     time.Now(),
	}

	if err := h.db.Create(&check).Error; err != nil {
		h.logger.Error("failed to save check result", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to save check result"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toCheckResponse(&check)})
}

// GetHistory returns the check history for a specific monitor.
func (h *CertHandler) GetHistory(c *gin.Context) {
	monitorID, err := strconv.ParseUint(c.Param("monitorId"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid monitor ID"))
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	var checks []domain.CertificateCheck
	if err := h.db.Where("monitor_id = ?", monitorID).
		Order("checked_at DESC").
		Limit(limit).
		Find(&checks).Error; err != nil {
		h.logger.Error("failed to get check history", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to get check history"))
		return
	}

	responses := make([]*checkResponse, 0, len(checks))
	for i := range checks {
		responses = append(responses, toCheckResponse(&checks[i]))
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

// --- Helpers ---

func toCheckResponse(check *domain.CertificateCheck) *checkResponse {
	var sans []string
	if check.SANs != "" {
		_ = json.Unmarshal([]byte(check.SANs), &sans)
	}
	if sans == nil {
		sans = []string{}
	}

	return &checkResponse{
		ID:            check.ID,
		Subject:       check.Subject,
		Issuer:        check.Issuer,
		ValidFrom:     check.ValidFrom,
		ValidTo:       check.ValidTo,
		DaysRemaining: check.DaysRemaining,
		SANs:          sans,
		ChainComplete: check.ChainComplete,
		Error:         check.Error,
		CheckedAt:     check.CheckedAt,
	}
}
