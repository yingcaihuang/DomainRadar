package dashboard

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

// DashboardHandler provides dashboard and reporting API endpoints.
type DashboardHandler struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(db *gorm.DB, logger *zap.Logger) *DashboardHandler {
	return &DashboardHandler{
		db:     db,
		logger: logger,
	}
}

// RegisterRoutes registers dashboard and reporting endpoints.
func (h *DashboardHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/dashboard", auth.RequirePermission(auth.ViewDomains), h.GetDashboard)
	rg.GET("/dashboard/health-scores", auth.RequirePermission(auth.ViewDomains), h.GetHealthScores)
	rg.GET("/reports/costs", auth.RequirePermission(auth.ViewDomains), h.GetCostStatistics)
	rg.GET("/audit-logs", auth.RequirePermission(auth.ViewAuditLogs), h.GetAuditLogs)
}

// --- Response types ---

// DashboardResponse represents the dashboard summary.
type DashboardResponse struct {
	TotalDomains     int64               `json:"total_domains"`
	ExpiringWithin30 int64               `json:"expiring_within_30_days"`
	ActiveAlerts     int64               `json:"active_alerts"`
	OverallHealth    float64             `json:"overall_health_score"`
	ByRegistrar      []RegistrarGroup    `json:"by_registrar"`
}

// RegistrarGroup shows domains grouped by registrar.
type RegistrarGroup struct {
	Registrar   string `json:"registrar"`
	DomainCount int64  `json:"domain_count"`
}

// HealthScoreEntry represents a domain's health score.
type HealthScoreEntry struct {
	ID          uint   `json:"id"`
	DomainName  string `json:"domain_name"`
	HealthScore int    `json:"health_score"`
}

// CostEntry represents costs grouped by registrar and period.
type CostEntry struct {
	Registrar string `json:"registrar"`
	Period    string `json:"period"`
	Tag       string `json:"tag,omitempty"`
	Count     int64  `json:"domain_count"`
}

// AuditLogResponse represents a paginated list of audit logs.
type AuditLogResponse struct {
	Logs       []domain.AuditLog `json:"logs"`
	Total      int64             `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// --- Handlers ---

// GetDashboard returns summary statistics.
func (h *DashboardHandler) GetDashboard(c *gin.Context) {
	ctx := c.Request.Context()

	// Total domains.
	var totalDomains int64
	h.db.WithContext(ctx).Model(&domain.NormalizedDomain{}).Count(&totalDomains)

	// Expiring within 30 days (includes already expired).
	var expiringWithin30 int64
	in30Days := time.Now().Add(30 * 24 * time.Hour)
	h.db.WithContext(ctx).Model(&domain.NormalizedDomain{}).
		Where("expiration_date IS NOT NULL AND expiration_date <= ?", in30Days).
		Count(&expiringWithin30)

	// Active alerts (unacknowledged).
	var activeAlerts int64
	h.db.WithContext(ctx).Model(&domain.Alert{}).
		Where("acknowledged = ?", false).
		Count(&activeAlerts)

	// Overall health score (average).
	var avgHealth float64
	h.db.WithContext(ctx).Model(&domain.NormalizedDomain{}).
		Select("COALESCE(AVG(health_score), 100)").
		Row().Scan(&avgHealth)

	// Domains by registrar.
	var registrarGroups []RegistrarGroup
	h.db.WithContext(ctx).Model(&domain.NormalizedDomain{}).
		Select("registrar_identifier as registrar, COUNT(*) as domain_count").
		Where("registrar_identifier != ''").
		Group("registrar_identifier").
		Order("domain_count DESC").
		Scan(&registrarGroups)

	c.JSON(http.StatusOK, DashboardResponse{
		TotalDomains:     totalDomains,
		ExpiringWithin30: expiringWithin30,
		ActiveAlerts:     activeAlerts,
		OverallHealth:    float64(int(avgHealth*100)) / 100,
		ByRegistrar:      registrarGroups,
	})
}

// GetHealthScores returns health scores for all domains.
func (h *DashboardHandler) GetHealthScores(c *gin.Context) {
	var entries []HealthScoreEntry
	if err := h.db.WithContext(c.Request.Context()).
		Model(&domain.NormalizedDomain{}).
		Select("id, domain_name, health_score").
		Order("health_score ASC").
		Find(&entries).Error; err != nil {
		h.logger.Error("failed to fetch health scores", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to fetch health scores"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": entries})
}

// GetCostStatistics returns renewal cost data grouped by registrar and period.
func (h *DashboardHandler) GetCostStatistics(c *gin.Context) {
	ctx := c.Request.Context()
	period := c.DefaultQuery("period", "yearly")

	// Group domains by registrar with counts for cost estimation.
	var costEntries []CostEntry
	h.db.WithContext(ctx).Model(&domain.NormalizedDomain{}).
		Select("registrar_identifier as registrar, COUNT(*) as count").
		Where("registrar_identifier != ''").
		Group("registrar_identifier").
		Order("count DESC").
		Scan(&costEntries)

	// Set period on all entries.
	for i := range costEntries {
		costEntries[i].Period = period
	}

	c.JSON(http.StatusOK, gin.H{"data": costEntries, "period": period})
}

// GetAuditLogs returns paginated audit logs (admin only).
func (h *DashboardHandler) GetAuditLogs(c *gin.Context) {
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

	query := h.db.Model(&domain.AuditLog{})

	// Filters.
	if userID := c.Query("user_id"); userID != "" {
		if uid, err := strconv.ParseUint(userID, 10, 64); err == nil {
			query = query.Where("user_id = ?", uid)
		}
	}
	if actionType := c.Query("action_type"); actionType != "" {
		query = query.Where("action_type = ?", actionType)
	}
	if resourceType := c.Query("resource_type"); resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	if dateFrom := c.Query("date_from"); dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if dateTo := c.Query("date_to"); dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			query = query.Where("created_at <= ?", t.Add(24*time.Hour))
		}
	}

	var total int64
	query.Count(&total)

	offset := (page - 1) * pageSize
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	var logs []domain.AuditLog
	query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs)

	c.JSON(http.StatusOK, AuditLogResponse{
		Logs:       logs,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}
