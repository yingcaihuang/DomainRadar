package alert

import (
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

// AlertHandler implements the alert API endpoints.
type AlertHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAlertHandler creates a new AlertHandler with the given dependencies.
func NewAlertHandler(db *gorm.DB, logger *zap.Logger) *AlertHandler {
	return &AlertHandler{
		db:     db,
		logger: logger,
	}
}

// alertListResponse is the response body for listing alerts.
type alertListResponse struct {
	Alerts     []alertResponse `json:"alerts"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
}

// alertResponse represents a single alert in API responses.
type alertResponse struct {
	ID             uint        `json:"id"`
	DomainID       uint        `json:"domain_id"`
	DomainName     string      `json:"domain_name,omitempty"`
	AlertType      string      `json:"alert_type"`
	Severity       string      `json:"severity"`
	Message        string      `json:"message"`
	DaysRemaining  *int        `json:"days_remaining"`
	Acknowledged   bool        `json:"acknowledged"`
	DeliveryStatus string      `json:"delivery_status"`
	GeneratedAt    string      `json:"generated_at"`
	AcknowledgedAt *string     `json:"acknowledged_at"`
}

// HandleListAlerts lists alerts with filtering and pagination.
// GET /api/v1/alerts
func (h *AlertHandler) HandleListAlerts(c *gin.Context) {
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

	// Build query with filters
	query := h.db.Model(&domain.Alert{})

	// Filter by severity
	if severity := c.Query("severity"); severity != "" {
		query = query.Where("severity = ?", severity)
	}

	// Filter by alert type
	if alertType := c.Query("alert_type"); alertType != "" {
		query = query.Where("alert_type = ?", alertType)
	}

	// Filter by acknowledged status
	if ack := c.Query("acknowledged"); ack != "" {
		if ack == "true" {
			query = query.Where("acknowledged = ?", true)
		} else if ack == "false" {
			query = query.Where("acknowledged = ?", false)
		}
	}

	// Filter by date range
	if dateFrom := c.Query("date_from"); dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query = query.Where("generated_at >= ?", t)
		}
	}
	if dateTo := c.Query("date_to"); dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			// Include the entire day by setting to end of day
			endOfDay := t.Add(24*time.Hour - time.Nanosecond)
			query = query.Where("generated_at <= ?", endOfDay)
		}
	}

	// Count total matching records
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count alerts", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve alerts"))
		return
	}

	// Calculate pagination
	offset := (page - 1) * pageSize
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	// Fetch alerts with preloaded domain name, sorted by generated_at DESC
	var alerts []domain.Alert
	if err := query.
		Preload("Domain").
		Order("generated_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&alerts).Error; err != nil {
		h.logger.Error("failed to list alerts", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve alerts"))
		return
	}

	// Build response
	alertResponses := make([]alertResponse, 0, len(alerts))
	for _, a := range alerts {
		alertResponses = append(alertResponses, toAlertResponse(a))
	}

	c.JSON(http.StatusOK, alertListResponse{
		Alerts:     alertResponses,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// HandleGetAlert returns a single alert by ID.
// GET /api/v1/alerts/:id
func (h *AlertHandler) HandleGetAlert(c *gin.Context) {
	idStr := c.Param("id")
	alertID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid alert ID"))
		return
	}

	var alert domain.Alert
	if err := h.db.Preload("Domain").First(&alert, uint(alertID)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("alert not found"))
			return
		}
		h.logger.Error("failed to get alert", zap.Error(err), zap.Uint64("alert_id", alertID))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve alert"))
		return
	}

	c.JSON(http.StatusOK, toAlertResponse(alert))
}

// HandleAcknowledgeAlert marks an alert as acknowledged.
// PUT /api/v1/alerts/:id/acknowledge
func (h *AlertHandler) HandleAcknowledgeAlert(c *gin.Context) {
	idStr := c.Param("id")
	alertID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid alert ID"))
		return
	}

	var alert domain.Alert
	if err := h.db.First(&alert, uint(alertID)).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("alert not found"))
			return
		}
		h.logger.Error("failed to find alert for acknowledgment", zap.Error(err), zap.Uint64("alert_id", alertID))
		errors.ErrorResponse(c, errors.InternalServer("failed to acknowledge alert"))
		return
	}

	if alert.Acknowledged {
		errors.ErrorResponse(c, errors.BadRequest("alert already acknowledged"))
		return
	}

	now := time.Now()
	if err := h.db.Model(&alert).Updates(map[string]interface{}{
		"acknowledged":    true,
		"acknowledged_at": now,
	}).Error; err != nil {
		h.logger.Error("failed to acknowledge alert", zap.Error(err), zap.Uint64("alert_id", alertID))
		errors.ErrorResponse(c, errors.InternalServer("failed to acknowledge alert"))
		return
	}

	// Reload alert with updated fields
	alert.Acknowledged = true
	alert.AcknowledgedAt = &now

	c.JSON(http.StatusOK, gin.H{
		"message": "alert acknowledged successfully",
		"alert":   toAlertResponse(alert),
	})
}

// RegisterRoutes registers alert routes on the given router group.
func (h *AlertHandler) RegisterRoutes(group *gin.RouterGroup) {
	alerts := group.Group("/alerts")
	{
		alerts.GET("", auth.RequirePermission(auth.ViewAlerts), h.HandleListAlerts)
		alerts.GET("/:id", auth.RequirePermission(auth.ViewAlerts), h.HandleGetAlert)
		alerts.PUT("/:id/acknowledge", auth.RequirePermission(auth.ManageAlerts), h.HandleAcknowledgeAlert)
	}
}

// toAlertResponse converts a domain.Alert to the API response format.
func toAlertResponse(a domain.Alert) alertResponse {
	resp := alertResponse{
		ID:             a.ID,
		DomainID:       a.DomainID,
		AlertType:      a.AlertType,
		Severity:       a.Severity,
		Message:        a.Message,
		DaysRemaining:  a.DaysRemaining,
		Acknowledged:   a.Acknowledged,
		DeliveryStatus: a.DeliveryStatus,
		GeneratedAt:    a.GeneratedAt.Format(time.RFC3339),
	}

	if a.AcknowledgedAt != nil {
		formatted := a.AcknowledgedAt.Format(time.RFC3339)
		resp.AcknowledgedAt = &formatted
	}

	// Include domain name if the Domain relation was preloaded
	if a.Domain.DomainName != "" {
		resp.DomainName = a.Domain.DomainName
	}

	return resp
}
