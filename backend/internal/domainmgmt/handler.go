package domainmgmt

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"domainradar/internal/audit"
	"domainradar/internal/auth"
	"domainradar/internal/domain"
	"domainradar/internal/errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// MaxImportFileSize is the maximum file size for CSV/Excel import (10MB).
	MaxImportFileSize = 10 * 1024 * 1024

	// MaxImportRows is the maximum number of rows in an import file.
	MaxImportRows = 5000

	// MaxExportRows is the maximum number of rows in an export.
	MaxExportRows = 10000

	// MaxBulkOperations is the maximum number of domains in a single bulk operation.
	MaxBulkOperations = 500

	// MaxTagsPerDomain is the maximum number of tags per domain.
	MaxTagsPerDomain = 20

	// MaxTagNameLength is the maximum length of a tag name.
	MaxTagNameLength = 50

	// MaxGroupLevels is the maximum nesting depth for groups.
	MaxGroupLevels = 3
)

// DomainHandler implements domain CRUD, import, export, and bulk operations.
type DomainHandler struct {
	db           *gorm.DB
	auditService *audit.Service
	logger       *zap.Logger
}

// NewDomainHandler creates a new DomainHandler.
func NewDomainHandler(db *gorm.DB, auditService *audit.Service, logger *zap.Logger) *DomainHandler {
	return &DomainHandler{
		db:           db,
		auditService: auditService,
		logger:       logger,
	}
}

// RegisterRoutes registers domain endpoints on the given Gin router group.
func (h *DomainHandler) RegisterRoutes(rg *gin.RouterGroup) {
	domains := rg.Group("/domains")
	{
		// Read operations — all authenticated users.
		domains.GET("", auth.RequirePermission(auth.ViewDomains), h.ListDomains)
		domains.GET("/calendar", auth.RequirePermission(auth.ViewDomains), h.GetCalendar)
		domains.GET("/export", auth.RequirePermission(auth.ViewDomains), h.ExportDomains)
		domains.GET("/:id", auth.RequirePermission(auth.ViewDomains), h.GetDomain)
		domains.GET("/:id/whois", auth.RequirePermission(auth.ViewDomains), h.GetWhoisInfo)

		// Write operations — operator+.
		domains.POST("", auth.RequirePermission(auth.ManageDomains), h.CreateDomain)
		domains.PUT("/:id", auth.RequirePermission(auth.ManageDomains), h.UpdateDomain)
		domains.DELETE("/:id", auth.RequirePermission(auth.ManageDomains), h.DeleteDomain)
		domains.POST("/import", auth.RequirePermission(auth.ManageDomains), h.ImportDomains)
		domains.POST("/bulk", auth.RequirePermission(auth.ManageDomains), h.BulkOperation)
	}

	// Tags.
	tags := rg.Group("/tags")
	{
		tags.GET("", auth.RequirePermission(auth.ViewDomains), h.ListTags)
		tags.POST("", auth.RequirePermission(auth.ManageDomains), h.CreateTag)
		tags.DELETE("/:id", auth.RequirePermission(auth.ManageDomains), h.DeleteTag)
	}

	// Groups.
	groups := rg.Group("/groups")
	{
		groups.GET("", auth.RequirePermission(auth.ViewDomains), h.ListGroups)
		groups.POST("", auth.RequirePermission(auth.ManageDomains), h.CreateGroup)
		groups.PUT("/:id", auth.RequirePermission(auth.ManageDomains), h.UpdateGroup)
		groups.DELETE("/:id", auth.RequirePermission(auth.ManageDomains), h.DeleteGroup)
	}
}

// --- Request/Response types ---

type DomainListResponse struct {
	Domains    []DomainResponse `json:"domains"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
}

type DomainResponse struct {
	ID                      uint            `json:"id"`
	DomainName              string          `json:"domain_name"`
	RegistrarAccountID      *uint           `json:"registrar_account_id"`
	RegistrarIdentifier     string          `json:"registrar_identifier"`
	CreationDate            *time.Time      `json:"creation_date"`
	ExpirationDate          *time.Time      `json:"expiration_date"`
	AutoRenew               bool            `json:"auto_renew"`
	RenewalDeadline         *time.Time      `json:"renewal_deadline"`
	Status                  string          `json:"status"`
	Nameservers             []string        `json:"nameservers"`
	PrivacyProtection       bool            `json:"privacy_protection"`
	LockStatus              bool            `json:"lock_status"`
	DataSourceType          string          `json:"data_source_type"`
	LastSyncAt              *time.Time      `json:"last_sync_at"`
	GroupID                 *uint           `json:"group_id"`
	Notes                   string          `json:"notes"`
	WebsiteURL              string          `json:"website_url"`
	EmailEnabled            bool            `json:"email_enabled"`
	HealthScore             int             `json:"health_score"`
	CheckIntervalMinutes    int             `json:"check_interval_minutes"`
	ResponseTimeThresholdMs int             `json:"response_time_threshold_ms"`
	Tags                    []TagResponse   `json:"tags"`
	Group                   *GroupResponse  `json:"group,omitempty"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

type TagResponse struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type GroupResponse struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	ParentID *uint  `json:"parent_id"`
	Level    int    `json:"level"`
}

type CreateDomainRequest struct {
	DomainName          string     `json:"domain_name" binding:"required"`
	RegistrarIdentifier string     `json:"registrar_identifier"`
	CreationDate        *time.Time `json:"creation_date"`
	ExpirationDate      *time.Time `json:"expiration_date"`
	AutoRenew           bool       `json:"auto_renew"`
	RenewalDeadline     *time.Time `json:"renewal_deadline"`
	Status              string     `json:"status"`
	Nameservers         []string   `json:"nameservers"`
	PrivacyProtection   bool       `json:"privacy_protection"`
	LockStatus          bool       `json:"lock_status"`
	GroupID             *uint      `json:"group_id"`
	Notes               string     `json:"notes"`
	WebsiteURL          string     `json:"website_url"`
	EmailEnabled        bool       `json:"email_enabled"`
	TagIDs              []uint     `json:"tag_ids"`
}

type UpdateDomainRequest struct {
	DomainName          *string    `json:"domain_name"`
	RegistrarIdentifier *string    `json:"registrar_identifier"`
	CreationDate        *time.Time `json:"creation_date"`
	ExpirationDate      *time.Time `json:"expiration_date"`
	AutoRenew           *bool      `json:"auto_renew"`
	RenewalDeadline     *time.Time `json:"renewal_deadline"`
	Status              *string    `json:"status"`
	Nameservers         *[]string  `json:"nameservers"`
	PrivacyProtection   *bool      `json:"privacy_protection"`
	LockStatus          *bool      `json:"lock_status"`
	GroupID             *uint      `json:"group_id"`
	Notes               *string    `json:"notes"`
	WebsiteURL          *string    `json:"website_url"`
	EmailEnabled        *bool      `json:"email_enabled"`
	TagIDs              *[]uint    `json:"tag_ids"`
}

type BulkRequest struct {
	DomainIDs []uint `json:"domain_ids" binding:"required"`
	Action    string `json:"action" binding:"required"` // "tag", "untag", "group", "delete"
	TagIDs    []uint `json:"tag_ids"`
	GroupID   *uint  `json:"group_id"`
}

// ImportResult represents the result of a CSV/Excel import.
type ImportResult struct {
	TotalRows   int           `json:"total_rows"`
	Created     int           `json:"created"`
	Updated     int           `json:"updated"`
	Errors      []ImportError `json:"errors"`
	TotalErrors int           `json:"total_errors"`
}

// ImportError represents a single row-level error during import.
type ImportError struct {
	Row    int    `json:"row"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// --- Domain CRUD ---

// ListDomains returns a paginated, filterable list of domains.
func (h *DomainHandler) ListDomains(c *gin.Context) {
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

	query := h.db.Model(&domain.NormalizedDomain{})

	// Filter by tags (comma-separated tag IDs).
	if tagIDs := c.Query("tag_ids"); tagIDs != "" {
		ids := parseUintSlice(tagIDs)
		if len(ids) > 0 {
			query = query.Where("id IN (SELECT domain_id FROM domain_tags WHERE tag_id IN ?)", ids)
		}
	}

	// Filter by group.
	if groupID := c.Query("group_id"); groupID != "" {
		if gid, err := strconv.ParseUint(groupID, 10, 64); err == nil {
			query = query.Where("group_id = ?", gid)
		}
	}

	// Filter by registrar.
	if registrar := c.Query("registrar"); registrar != "" {
		query = query.Where("registrar_identifier = ?", registrar)
	}

	// Filter by registrar account ID.
	if accountID := c.Query("registrar_account_id"); accountID != "" {
		query = query.Where("registrar_account_id = ?", accountID)
	}

	// Filter by status.
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	// Filter by data source type.
	if dsType := c.Query("data_source_type"); dsType != "" {
		query = query.Where("data_source_type = ?", dsType)
	}

	// Filter by expiration date range.
	if expiresFrom := c.Query("expires_from"); expiresFrom != "" {
		if t, err := time.Parse("2006-01-02", expiresFrom); err == nil {
			query = query.Where("expiration_date >= ?", t)
		}
	}
	if expiresTo := c.Query("expires_to"); expiresTo != "" {
		if t, err := time.Parse("2006-01-02", expiresTo); err == nil {
			query = query.Where("expiration_date <= ?", t.Add(24*time.Hour-time.Nanosecond))
		}
	}

	// Search by domain name.
	if search := c.Query("search"); search != "" {
		query = query.Where("domain_name ILIKE ?", "%"+search+"%")
	}

	// Count total.
	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.logger.Error("failed to count domains", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve domains"))
		return
	}

	offset := (page - 1) * pageSize
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	// Determine sort order.
	sortBy := c.DefaultQuery("sort_by", "expiration_date")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	allowedSorts := map[string]bool{"domain_name": true, "expiration_date": true, "creation_date": true, "status": true, "health_score": true}
	if !allowedSorts[sortBy] {
		sortBy = "expiration_date"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "asc"
	}
	orderClause := sortBy + " " + sortOrder
	if sortBy == "expiration_date" {
		// NULL expiration dates go last
		orderClause = "expiration_date IS NULL, " + orderClause
	}

	// Fetch domains.
	var domains []domain.NormalizedDomain
	if err := query.
		Preload("Tags").
		Preload("Group").
		Order(orderClause).
		Offset(offset).
		Limit(pageSize).
		Find(&domains).Error; err != nil {
		h.logger.Error("failed to list domains", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve domains"))
		return
	}

	responses := make([]DomainResponse, 0, len(domains))
	for _, d := range domains {
		responses = append(responses, toDomainResponse(d))
	}

	c.JSON(http.StatusOK, DomainListResponse{
		Domains:    responses,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// GetDomain returns a single domain by ID.
func (h *DomainHandler) GetDomain(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var d domain.NormalizedDomain
	if err := h.db.Preload("Tags").Preload("Group").First(&d, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("domain not found"))
			return
		}
		h.logger.Error("failed to get domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to retrieve domain"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toDomainResponse(d)})
}

// CreateDomain creates a new domain record.
func (h *DomainHandler) CreateDomain(c *gin.Context) {
	var req CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	if len(req.DomainName) > 253 {
		errors.ErrorResponse(c, errors.BadRequest("domain name exceeds 253 characters"))
		return
	}

	// Check uniqueness.
	var count int64
	h.db.Model(&domain.NormalizedDomain{}).Where("domain_name = ?", req.DomainName).Count(&count)
	if count > 0 {
		errors.ErrorResponse(c, errors.Conflict("domain already exists"))
		return
	}

	// Validate tag count.
	if len(req.TagIDs) > MaxTagsPerDomain {
		errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("cannot assign more than %d tags", MaxTagsPerDomain)))
		return
	}

	d := domain.NormalizedDomain{
		DomainName:          req.DomainName,
		RegistrarIdentifier: req.RegistrarIdentifier,
		CreationDate:        req.CreationDate,
		ExpirationDate:      req.ExpirationDate,
		AutoRenew:           req.AutoRenew,
		RenewalDeadline:     req.RenewalDeadline,
		Status:              req.Status,
		Nameservers:         domain.JSON(req.Nameservers),
		PrivacyProtection:   req.PrivacyProtection,
		LockStatus:          req.LockStatus,
		DataSourceType:      "manual",
		GroupID:             req.GroupID,
		Notes:               req.Notes,
		WebsiteURL:          req.WebsiteURL,
		EmailEnabled:        req.EmailEnabled,
	}

	if err := h.db.Create(&d).Error; err != nil {
		h.logger.Error("failed to create domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create domain"))
		return
	}

	// Assign tags.
	if len(req.TagIDs) > 0 {
		var tags []domain.Tag
		h.db.Where("id IN ?", req.TagIDs).Find(&tags)
		h.db.Model(&d).Association("Tags").Replace(tags)
	}

	// Reload with associations.
	h.db.Preload("Tags").Preload("Group").First(&d, d.ID)

	userID := getUserID(c)
	h.auditService.RecordAction(userID, "CREATE", "domain", strconv.Itoa(int(d.ID)), map[string]interface{}{
		"domain_name": d.DomainName,
		"data_source": "manual",
	})

	c.JSON(http.StatusCreated, gin.H{"data": toDomainResponse(d)})
}

// UpdateDomain updates an existing domain record.
func (h *DomainHandler) UpdateDomain(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var d domain.NormalizedDomain
	if err := h.db.First(&d, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("domain not found"))
			return
		}
		h.logger.Error("failed to query domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query domain"))
		return
	}

	var req UpdateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	changes := make(map[string]interface{})

	if req.DomainName != nil {
		d.DomainName = *req.DomainName
		changes["domain_name"] = *req.DomainName
	}
	if req.RegistrarIdentifier != nil {
		d.RegistrarIdentifier = *req.RegistrarIdentifier
		changes["registrar_identifier"] = *req.RegistrarIdentifier
	}
	if req.CreationDate != nil {
		d.CreationDate = req.CreationDate
		changes["creation_date"] = req.CreationDate
	}
	if req.ExpirationDate != nil {
		d.ExpirationDate = req.ExpirationDate
		changes["expiration_date"] = req.ExpirationDate
	}
	if req.AutoRenew != nil {
		d.AutoRenew = *req.AutoRenew
		changes["auto_renew"] = *req.AutoRenew
	}
	if req.RenewalDeadline != nil {
		d.RenewalDeadline = req.RenewalDeadline
		changes["renewal_deadline"] = req.RenewalDeadline
	}
	if req.Status != nil {
		d.Status = *req.Status
		changes["status"] = *req.Status
	}
	if req.Nameservers != nil {
		d.Nameservers = domain.JSON(*req.Nameservers)
		changes["nameservers"] = *req.Nameservers
	}
	if req.PrivacyProtection != nil {
		d.PrivacyProtection = *req.PrivacyProtection
		changes["privacy_protection"] = *req.PrivacyProtection
	}
	if req.LockStatus != nil {
		d.LockStatus = *req.LockStatus
		changes["lock_status"] = *req.LockStatus
	}
	if req.GroupID != nil {
		d.GroupID = req.GroupID
		changes["group_id"] = *req.GroupID
	}
	if req.Notes != nil {
		d.Notes = *req.Notes
		changes["notes"] = *req.Notes
	}
	if req.WebsiteURL != nil {
		d.WebsiteURL = *req.WebsiteURL
		changes["website_url"] = *req.WebsiteURL
	}
	if req.EmailEnabled != nil {
		d.EmailEnabled = *req.EmailEnabled
		changes["email_enabled"] = *req.EmailEnabled
	}

	if err := h.db.Save(&d).Error; err != nil {
		h.logger.Error("failed to update domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to update domain"))
		return
	}

	// Update tags if specified.
	if req.TagIDs != nil {
		if len(*req.TagIDs) > MaxTagsPerDomain {
			errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("cannot assign more than %d tags", MaxTagsPerDomain)))
			return
		}
		var tags []domain.Tag
		if len(*req.TagIDs) > 0 {
			h.db.Where("id IN ?", *req.TagIDs).Find(&tags)
		}
		h.db.Model(&d).Association("Tags").Replace(tags)
		changes["tags"] = *req.TagIDs
	}

	h.db.Preload("Tags").Preload("Group").First(&d, d.ID)

	userID := getUserID(c)
	h.auditService.RecordAction(userID, "UPDATE", "domain", strconv.Itoa(int(d.ID)), changes)

	c.JSON(http.StatusOK, gin.H{"data": toDomainResponse(d)})
}

// DeleteDomain deletes a domain record.
func (h *DomainHandler) DeleteDomain(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var d domain.NormalizedDomain
	if err := h.db.First(&d, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("domain not found"))
			return
		}
		h.logger.Error("failed to query domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query domain"))
		return
	}

	h.db.Model(&d).Association("Tags").Clear()

	if err := h.db.Delete(&d).Error; err != nil {
		h.logger.Error("failed to delete domain", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to delete domain"))
		return
	}

	userID := getUserID(c)
	h.auditService.RecordAction(userID, "DELETE", "domain", strconv.Itoa(int(d.ID)), map[string]interface{}{
		"domain_name": d.DomainName,
	})

	c.JSON(http.StatusOK, gin.H{"message": "domain deleted"})
}

// --- Import/Export ---

// ImportDomains handles CSV file import.
func (h *DomainHandler) ImportDomains(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("file is required"))
		return
	}
	defer file.Close()

	if header.Size > MaxImportFileSize {
		errors.ErrorResponse(c, errors.BadRequest("file size exceeds 10MB limit"))
		return
	}

	reader := csv.NewReader(file)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("failed to read CSV header"))
		return
	}

	headerMap := make(map[string]int)
	for i, h := range headers {
		headerMap[strings.ToLower(strings.TrimSpace(h))] = i
	}

	if _, ok := headerMap["domain_name"]; !ok {
		if _, ok := headerMap["domain"]; !ok {
			errors.ErrorResponse(c, errors.BadRequest("CSV must contain a 'domain_name' or 'domain' column"))
			return
		}
		headerMap["domain_name"] = headerMap["domain"]
	}

	result := ImportResult{}
	rowNum := 1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, ImportError{Row: rowNum + 1, Field: "", Reason: "failed to parse row"})
			result.TotalErrors++
			rowNum++
			continue
		}

		rowNum++
		result.TotalRows++

		if result.TotalRows > MaxImportRows {
			errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("import exceeds maximum of %d rows", MaxImportRows)))
			return
		}

		domainName := ""
		if idx, ok := headerMap["domain_name"]; ok && idx < len(record) {
			domainName = strings.TrimSpace(record[idx])
		}
		if domainName == "" {
			result.Errors = append(result.Errors, ImportError{Row: rowNum, Field: "domain_name", Reason: "domain name is required"})
			result.TotalErrors++
			continue
		}

		var expirationDate *time.Time
		if idx, ok := headerMap["expiration_date"]; ok && idx < len(record) {
			dateStr := strings.TrimSpace(record[idx])
			if dateStr != "" {
				t, err := parseDate(dateStr)
				if err != nil {
					result.Errors = append(result.Errors, ImportError{Row: rowNum, Field: "expiration_date", Reason: "invalid date format"})
					result.TotalErrors++
					continue
				}
				expirationDate = &t
			}
		}
		if expirationDate == nil {
			result.Errors = append(result.Errors, ImportError{Row: rowNum, Field: "expiration_date", Reason: "expiration date is required"})
			result.TotalErrors++
			continue
		}

		var existing domain.NormalizedDomain
		err = h.db.Where("domain_name = ?", domainName).First(&existing).Error

		if err == nil {
			existing.ExpirationDate = expirationDate
			if idx, ok := headerMap["registrar"]; ok && idx < len(record) {
				existing.RegistrarIdentifier = strings.TrimSpace(record[idx])
			}
			if idx, ok := headerMap["auto_renew"]; ok && idx < len(record) {
				existing.AutoRenew = parseBool(record[idx])
			}
			h.db.Save(&existing)
			result.Updated++
		} else {
			d := domain.NormalizedDomain{
				DomainName:     domainName,
				ExpirationDate: expirationDate,
				DataSourceType: "manual",
			}
			if idx, ok := headerMap["registrar"]; ok && idx < len(record) {
				d.RegistrarIdentifier = strings.TrimSpace(record[idx])
			}
			if idx, ok := headerMap["auto_renew"]; ok && idx < len(record) {
				d.AutoRenew = parseBool(record[idx])
			}
			if idx, ok := headerMap["nameservers"]; ok && idx < len(record) {
				ns := strings.TrimSpace(record[idx])
				if ns != "" {
					d.Nameservers = domain.JSON(strings.Split(ns, ","))
				}
			}
			if idx, ok := headerMap["notes"]; ok && idx < len(record) {
				d.Notes = strings.TrimSpace(record[idx])
			}
			if err := h.db.Create(&d).Error; err != nil {
				result.Errors = append(result.Errors, ImportError{Row: rowNum, Field: "domain_name", Reason: "failed to create: " + err.Error()})
				result.TotalErrors++
				continue
			}
			result.Created++
		}
	}

	userID := getUserID(c)
	h.auditService.RecordAction(userID, "CREATE", "domain_import", "batch", map[string]interface{}{
		"total_rows": result.TotalRows,
		"created":    result.Created,
		"updated":    result.Updated,
		"errors":     result.TotalErrors,
	})

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// ExportDomains exports domains as CSV.
func (h *DomainHandler) ExportDomains(c *gin.Context) {
	query := h.db.Model(&domain.NormalizedDomain{}).Preload("Tags").Preload("Group")

	if tagIDs := c.Query("tag_ids"); tagIDs != "" {
		ids := parseUintSlice(tagIDs)
		if len(ids) > 0 {
			query = query.Where("id IN (SELECT domain_id FROM domain_tags WHERE tag_id IN ?)", ids)
		}
	}
	if groupID := c.Query("group_id"); groupID != "" {
		if gid, err := strconv.ParseUint(groupID, 10, 64); err == nil {
			query = query.Where("group_id = ?", gid)
		}
	}
	if registrar := c.Query("registrar"); registrar != "" {
		query = query.Where("registrar_identifier = ?", registrar)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	var domains []domain.NormalizedDomain
	if err := query.Order("domain_name ASC").Limit(MaxExportRows).Find(&domains).Error; err != nil {
		h.logger.Error("failed to export domains", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to export domains"))
		return
	}

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=domains_export.csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	writer.Write([]string{
		"domain_name", "registrar", "expiration_date", "creation_date",
		"auto_renew", "status", "nameservers", "data_source_type",
		"health_score", "tags", "group", "notes",
	})

	for _, d := range domains {
		row := EncodeExportRow(d)
		writer.Write(row)
	}
}

// --- Bulk Operations ---

// BulkOperation performs bulk tag/group/delete on up to 500 domains.
func (h *DomainHandler) BulkOperation(c *gin.Context) {
	var req BulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	if len(req.DomainIDs) == 0 {
		errors.ErrorResponse(c, errors.BadRequest("domain_ids is required"))
		return
	}
	if len(req.DomainIDs) > MaxBulkOperations {
		errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("cannot operate on more than %d domains at once", MaxBulkOperations)))
		return
	}

	userID := getUserID(c)

	switch req.Action {
	case "tag":
		if len(req.TagIDs) == 0 {
			errors.ErrorResponse(c, errors.BadRequest("tag_ids required for tag action"))
			return
		}
		var tags []domain.Tag
		h.db.Where("id IN ?", req.TagIDs).Find(&tags)
		for _, domainID := range req.DomainIDs {
			var d domain.NormalizedDomain
			if h.db.First(&d, domainID).Error == nil {
				h.db.Model(&d).Association("Tags").Append(tags)
			}
		}
		h.auditService.RecordAction(userID, "UPDATE", "domain_bulk_tag", "batch", map[string]interface{}{
			"domain_count": len(req.DomainIDs),
			"tag_ids":      req.TagIDs,
		})

	case "untag":
		if len(req.TagIDs) == 0 {
			errors.ErrorResponse(c, errors.BadRequest("tag_ids required for untag action"))
			return
		}
		var tags []domain.Tag
		h.db.Where("id IN ?", req.TagIDs).Find(&tags)
		for _, domainID := range req.DomainIDs {
			var d domain.NormalizedDomain
			if h.db.First(&d, domainID).Error == nil {
				h.db.Model(&d).Association("Tags").Delete(tags)
			}
		}
		h.auditService.RecordAction(userID, "UPDATE", "domain_bulk_untag", "batch", map[string]interface{}{
			"domain_count": len(req.DomainIDs),
			"tag_ids":      req.TagIDs,
		})

	case "group":
		if req.GroupID == nil {
			errors.ErrorResponse(c, errors.BadRequest("group_id required for group action"))
			return
		}
		h.db.Model(&domain.NormalizedDomain{}).Where("id IN ?", req.DomainIDs).Update("group_id", *req.GroupID)
		h.auditService.RecordAction(userID, "UPDATE", "domain_bulk_group", "batch", map[string]interface{}{
			"domain_count": len(req.DomainIDs),
			"group_id":     *req.GroupID,
		})

	case "delete":
		h.db.Where("id IN ?", req.DomainIDs).Delete(&domain.NormalizedDomain{})
		h.auditService.RecordAction(userID, "DELETE", "domain_bulk_delete", "batch", map[string]interface{}{
			"domain_count": len(req.DomainIDs),
		})

	default:
		errors.ErrorResponse(c, errors.BadRequest("action must be one of: tag, untag, group, delete"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("bulk %s completed for %d domains", req.Action, len(req.DomainIDs)),
		"count":   len(req.DomainIDs),
	})
}

// --- Tags ---

func (h *DomainHandler) ListTags(c *gin.Context) {
	var tags []domain.Tag
	if err := h.db.Order("name ASC").Find(&tags).Error; err != nil {
		h.logger.Error("failed to list tags", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to list tags"))
		return
	}

	responses := make([]TagResponse, 0, len(tags))
	for _, t := range tags {
		responses = append(responses, TagResponse{ID: t.ID, Name: t.Name})
	}

	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (h *DomainHandler) CreateTag(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	if len(req.Name) > MaxTagNameLength {
		errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("tag name must not exceed %d characters", MaxTagNameLength)))
		return
	}
	if len(req.Name) < 1 {
		errors.ErrorResponse(c, errors.BadRequest("tag name must be at least 1 character"))
		return
	}

	tag := domain.Tag{Name: req.Name}
	if err := h.db.Create(&tag).Error; err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			errors.ErrorResponse(c, errors.Conflict("tag already exists"))
			return
		}
		h.logger.Error("failed to create tag", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create tag"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": TagResponse{ID: tag.ID, Name: tag.Name}})
}

func (h *DomainHandler) DeleteTag(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid tag ID"))
		return
	}

	var tag domain.Tag
	if err := h.db.First(&tag, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("tag not found"))
			return
		}
		errors.ErrorResponse(c, errors.InternalServer("failed to query tag"))
		return
	}

	h.db.Exec("DELETE FROM domain_tags WHERE tag_id = ?", tag.ID)
	h.db.Delete(&tag)

	c.JSON(http.StatusOK, gin.H{"message": "tag deleted"})
}

// --- Groups ---

func (h *DomainHandler) ListGroups(c *gin.Context) {
	var groups []domain.Group
	if err := h.db.Preload("Children").Order("level ASC, name ASC").Find(&groups).Error; err != nil {
		h.logger.Error("failed to list groups", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to list groups"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": groups})
}

func (h *DomainHandler) CreateGroup(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		ParentID *uint  `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	if len(req.Name) > 100 {
		errors.ErrorResponse(c, errors.BadRequest("group name must not exceed 100 characters"))
		return
	}

	level := 1
	if req.ParentID != nil {
		var parent domain.Group
		if err := h.db.First(&parent, *req.ParentID).Error; err != nil {
			errors.ErrorResponse(c, errors.BadRequest("parent group not found"))
			return
		}
		level = parent.Level + 1
		if level > MaxGroupLevels {
			errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("group nesting cannot exceed %d levels", MaxGroupLevels)))
			return
		}
	}

	group := domain.Group{
		Name:     req.Name,
		ParentID: req.ParentID,
		Level:    level,
	}

	if err := h.db.Create(&group).Error; err != nil {
		h.logger.Error("failed to create group", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to create group"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": GroupResponse{
		ID:       group.ID,
		Name:     group.Name,
		ParentID: group.ParentID,
		Level:    group.Level,
	}})
}

func (h *DomainHandler) UpdateGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid group ID"))
		return
	}

	var group domain.Group
	if err := h.db.First(&group, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("group not found"))
			return
		}
		errors.ErrorResponse(c, errors.InternalServer("failed to query group"))
		return
	}

	var req struct {
		Name     *string `json:"name"`
		ParentID *uint   `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid request body: "+err.Error()))
		return
	}

	if req.Name != nil {
		if len(*req.Name) > 100 {
			errors.ErrorResponse(c, errors.BadRequest("group name must not exceed 100 characters"))
			return
		}
		group.Name = *req.Name
	}

	if req.ParentID != nil {
		var parent domain.Group
		if err := h.db.First(&parent, *req.ParentID).Error; err != nil {
			errors.ErrorResponse(c, errors.BadRequest("parent group not found"))
			return
		}
		if parent.Level+1 > MaxGroupLevels {
			errors.ErrorResponse(c, errors.BadRequest(fmt.Sprintf("group nesting cannot exceed %d levels", MaxGroupLevels)))
			return
		}
		group.ParentID = req.ParentID
		group.Level = parent.Level + 1
	}

	if err := h.db.Save(&group).Error; err != nil {
		h.logger.Error("failed to update group", zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to update group"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": GroupResponse{
		ID:       group.ID,
		Name:     group.Name,
		ParentID: group.ParentID,
		Level:    group.Level,
	}})
}

func (h *DomainHandler) DeleteGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid group ID"))
		return
	}

	var group domain.Group
	if err := h.db.First(&group, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			errors.ErrorResponse(c, errors.NotFound("group not found"))
			return
		}
		errors.ErrorResponse(c, errors.InternalServer("failed to query group"))
		return
	}

	h.db.Model(&domain.NormalizedDomain{}).Where("group_id = ?", group.ID).Update("group_id", nil)
	h.db.Where("parent_id = ?", group.ID).Delete(&domain.Group{})
	h.db.Delete(&group)

	c.JSON(http.StatusOK, gin.H{"message": "group deleted"})
}

// --- Expiration Calendar ---

type CalendarEntry struct {
	ID             uint   `json:"id"`
	DomainName     string `json:"domain_name"`
	ExpirationDate string `json:"expiration_date"`
	Type           string `json:"type"` // "domain" or "certificate"
	Severity       string `json:"severity"`
	DaysRemaining  int    `json:"days_remaining"`
}

func (h *DomainHandler) GetCalendar(c *gin.Context) {
	year, _ := strconv.Atoi(c.DefaultQuery("year", strconv.Itoa(time.Now().Year())))
	month, _ := strconv.Atoi(c.DefaultQuery("month", strconv.Itoa(int(time.Now().Month()))))

	startDate := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(0, 1, 0)

	var domains []domain.NormalizedDomain
	h.db.Where("expiration_date >= ? AND expiration_date < ?", startDate, endDate).Find(&domains)

	entries := make([]CalendarEntry, 0)
	now := time.Now()

	for _, d := range domains {
		if d.ExpirationDate == nil {
			continue
		}
		daysRemaining := int(d.ExpirationDate.Sub(now).Hours() / 24)
		entries = append(entries, CalendarEntry{
			ID:             d.ID,
			DomainName:     d.DomainName,
			ExpirationDate: d.ExpirationDate.Format("2006-01-02"),
			Type:           "domain",
			Severity:       calcSeverity(daysRemaining),
			DaysRemaining:  daysRemaining,
		})
	}

	// Get certificate expirations in range (from certificate monitors).
	var certs []struct {
		DomainID   uint      `json:"domain_id"`
		DomainName string    `json:"domain_name"`
		Endpoint   string    `json:"endpoint"`
		ValidTo    time.Time `json:"valid_to"`
	}
	h.db.Raw(`SELECT DISTINCT ON (cc.domain_id, cc.monitor_id) cc.domain_id, d.domain_name, COALESCE(cm.endpoint, '') as endpoint, cc.valid_to
		FROM certificate_checks cc
		JOIN domains d ON d.id = cc.domain_id
		LEFT JOIN certificate_monitors cm ON cm.id = cc.monitor_id
		WHERE cc.valid_to >= ? AND cc.valid_to < ?
		ORDER BY cc.domain_id, cc.monitor_id, cc.checked_at DESC`, startDate, endDate).Scan(&certs)

	for _, cert := range certs {
		daysRemaining := int(cert.ValidTo.Sub(now).Hours() / 24)
		domainLabel := cert.DomainName
		if cert.Endpoint != "" {
			domainLabel = cert.DomainName + " (" + cert.Endpoint + ")"
		}
		entries = append(entries, CalendarEntry{
			ID:             cert.DomainID,
			DomainName:     domainLabel,
			ExpirationDate: cert.ValidTo.Format("2006-01-02"),
			Type:           "certificate",
			Severity:       calcSeverity(daysRemaining),
			DaysRemaining:  daysRemaining,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": entries})
}

// --- Helper functions ---

func toDomainResponse(d domain.NormalizedDomain) DomainResponse {
	tags := make([]TagResponse, 0, len(d.Tags))
	for _, t := range d.Tags {
		tags = append(tags, TagResponse{ID: t.ID, Name: t.Name})
	}

	var group *GroupResponse
	if d.Group != nil {
		group = &GroupResponse{
			ID:       d.Group.ID,
			Name:     d.Group.Name,
			ParentID: d.Group.ParentID,
			Level:    d.Group.Level,
		}
	}

	return DomainResponse{
		ID:                      d.ID,
		DomainName:              d.DomainName,
		RegistrarAccountID:      d.RegistrarAccountID,
		RegistrarIdentifier:     d.RegistrarIdentifier,
		CreationDate:            d.CreationDate,
		ExpirationDate:          d.ExpirationDate,
		AutoRenew:               d.AutoRenew,
		RenewalDeadline:         d.RenewalDeadline,
		Status:                  d.Status,
		Nameservers:             []string(d.Nameservers),
		PrivacyProtection:       d.PrivacyProtection,
		LockStatus:              d.LockStatus,
		DataSourceType:          d.DataSourceType,
		LastSyncAt:              d.LastSyncAt,
		GroupID:                 d.GroupID,
		Notes:                   d.Notes,
		WebsiteURL:              d.WebsiteURL,
		EmailEnabled:            d.EmailEnabled,
		HealthScore:             d.HealthScore,
		CheckIntervalMinutes:    d.CheckIntervalMinutes,
		ResponseTimeThresholdMs: d.ResponseTimeThresholdMs,
		Tags:                    tags,
		Group:                   group,
		CreatedAt:               d.CreatedAt,
		UpdatedAt:               d.UpdatedAt,
	}
}

func getUserID(c *gin.Context) uint {
	user, exists := c.Get("user")
	if !exists {
		return 0
	}
	if u, ok := user.(*auth.UserInfo); ok {
		return u.ID
	}
	return 0
}

func parseUintSlice(s string) []uint {
	parts := strings.Split(s, ",")
	var result []uint
	for _, p := range parts {
		if v, err := strconv.ParseUint(strings.TrimSpace(p), 10, 64); err == nil {
			result = append(result, uint(v))
		}
	}
	return result
}

func parseDate(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
		"02-01-2006",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date format: %s", s)
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "yes" || s == "1"
}

func calcSeverity(daysRemaining int) string {
	switch {
	case daysRemaining < 0:
		return "expired"
	case daysRemaining <= 7:
		return "critical"
	case daysRemaining <= 30:
		return "warning"
	default:
		return "informational"
	}
}

// --- Exported functions for property testing ---

// ValidateImportRow validates a single import row's required fields.
func ValidateImportRow(domainName string, expirationDate string) (string, *time.Time, *ImportError) {
	name := strings.TrimSpace(domainName)
	if name == "" {
		return "", nil, &ImportError{Field: "domain_name", Reason: "domain name is required"}
	}

	if expirationDate == "" {
		return name, nil, &ImportError{Field: "expiration_date", Reason: "expiration date is required"}
	}

	t, err := parseDate(expirationDate)
	if err != nil {
		return name, nil, &ImportError{Field: "expiration_date", Reason: "invalid date format"}
	}

	return name, &t, nil
}

// ValidateTagConstraints checks tag constraints: max 20 tags per domain, 1-50 char names.
func ValidateTagConstraints(tagCount int, tagName string) error {
	if tagCount > MaxTagsPerDomain {
		return fmt.Errorf("cannot assign more than %d tags", MaxTagsPerDomain)
	}
	if len(tagName) < 1 || len(tagName) > MaxTagNameLength {
		return fmt.Errorf("tag name must be 1-%d characters", MaxTagNameLength)
	}
	return nil
}

// ValidateGroupLevel checks if adding a child would exceed max nesting levels.
func ValidateGroupLevel(parentLevel int) error {
	if parentLevel+1 > MaxGroupLevels {
		return fmt.Errorf("group nesting cannot exceed %d levels", MaxGroupLevels)
	}
	return nil
}

// FilterDomains is a pure function that applies filters to a domain list.
func FilterDomains(domains []domain.NormalizedDomain, tagIDs []uint, groupID *uint, registrar, status string) []domain.NormalizedDomain {
	var result []domain.NormalizedDomain
	for _, d := range domains {
		if len(tagIDs) > 0 {
			hasTag := false
			for _, t := range d.Tags {
				for _, id := range tagIDs {
					if t.ID == id {
						hasTag = true
						break
					}
				}
				if hasTag {
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		if groupID != nil && (d.GroupID == nil || *d.GroupID != *groupID) {
			continue
		}

		if registrar != "" && d.RegistrarIdentifier != registrar {
			continue
		}

		if status != "" && d.Status != status {
			continue
		}

		result = append(result, d)
	}
	return result
}

// EncodeExportRow converts a domain to a CSV row including tags and groups.
func EncodeExportRow(d domain.NormalizedDomain) []string {
	expDate := ""
	if d.ExpirationDate != nil {
		expDate = d.ExpirationDate.Format("2006-01-02")
	}
	createDate := ""
	if d.CreationDate != nil {
		createDate = d.CreationDate.Format("2006-01-02")
	}

	tagNames := make([]string, 0, len(d.Tags))
	for _, t := range d.Tags {
		tagNames = append(tagNames, t.Name)
	}

	groupName := ""
	if d.Group != nil {
		groupName = d.Group.Name
	}

	return []string{
		d.DomainName,
		d.RegistrarIdentifier,
		expDate,
		createDate,
		strconv.FormatBool(d.AutoRenew),
		d.Status,
		strings.Join([]string(d.Nameservers), ","),
		d.DataSourceType,
		strconv.Itoa(d.HealthScore),
		strings.Join(tagNames, ","),
		groupName,
		d.Notes,
	}
}

// unused but needed to satisfy import
var _ = json.Marshal

// GetWhoisInfo fetches WHOIS/RDAP information for a domain from the who-dat service.
// GET /api/v1/domains/:id/whois
func (h *DomainHandler) GetWhoisInfo(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		errors.ErrorResponse(c, errors.BadRequest("invalid domain ID"))
		return
	}

	var d domain.NormalizedDomain
	if err := h.db.First(&d, id).Error; err != nil {
		errors.ErrorResponse(c, errors.NotFound("domain not found"))
		return
	}

	// Call who-dat service
	whoDatURL := os.Getenv("WHO_DAT_URL")
	if whoDatURL == "" {
		whoDatURL = "http://who-dat:8080"
	}

	resp, err := http.Get(whoDatURL + "/" + d.DomainName)
	if err != nil {
		h.logger.Error("failed to query who-dat", zap.String("domain", d.DomainName), zap.Error(err))
		errors.ErrorResponse(c, errors.InternalServer("failed to query WHOIS service"))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errors.ErrorResponse(c, errors.InternalServer("WHOIS service returned error"))
		return
	}

	var whoisData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&whoisData); err != nil {
		errors.ErrorResponse(c, errors.InternalServer("failed to parse WHOIS data"))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": whoisData})
}
