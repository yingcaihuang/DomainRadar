package audit

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"domainradar/internal/domain"
)

// sensitiveKeyPatterns contains substrings that indicate a credential-related field.
var sensitiveKeyPatterns = []string{
	"password",
	"secret",
	"key",
	"token",
	"credential",
}

// RetentionPeriod is the default retention duration for audit logs (365 days).
const RetentionPeriod = 365 * 24 * time.Hour

// Service handles recording and managing audit log entries.
type Service struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewService creates a new audit logger service.
func NewService(db *gorm.DB, logger *zap.Logger) *Service {
	return &Service{
		db:     db,
		logger: logger,
	}
}

// RecordAction records an audit log entry for a user action.
// Changed fields are stored as JSON with credential-related values masked.
func (s *Service) RecordAction(userID uint, actionType string, resourceType string, resourceID string, changedFields map[string]interface{}) error {
	masked := maskSensitiveFields(changedFields)

	fieldsJSON, err := json.Marshal(masked)
	if err != nil {
		s.logger.Error("failed to marshal changed fields for audit log",
			zap.Uint("user_id", userID),
			zap.String("action_type", actionType),
			zap.String("resource_type", resourceType),
			zap.String("resource_id", resourceID),
			zap.Error(err),
		)
		return err
	}

	entry := domain.AuditLog{
		UserID:        userID,
		ActionType:    actionType,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		ChangedFields: json.RawMessage(fieldsJSON),
		CreatedAt:     time.Now(),
	}

	if err := s.db.Create(&entry).Error; err != nil {
		s.logger.Error("failed to create audit log entry",
			zap.Uint("user_id", userID),
			zap.String("action_type", actionType),
			zap.String("resource_type", resourceType),
			zap.String("resource_id", resourceID),
			zap.Error(err),
		)
		return err
	}

	s.logger.Debug("audit log recorded",
		zap.Uint("user_id", userID),
		zap.String("action_type", actionType),
		zap.String("resource_type", resourceType),
		zap.String("resource_id", resourceID),
	)

	return nil
}

// CleanupOldRecords removes audit log entries older than the specified duration.
// For the 365-day retention policy, pass RetentionPeriod as olderThan.
func (s *Service) CleanupOldRecords(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	result := s.db.Where("created_at < ?", cutoff).Delete(&domain.AuditLog{})
	if result.Error != nil {
		s.logger.Error("failed to cleanup old audit log records",
			zap.Time("cutoff", cutoff),
			zap.Error(result.Error),
		)
		return 0, result.Error
	}

	if result.RowsAffected > 0 {
		s.logger.Info("cleaned up old audit log records",
			zap.Int64("deleted_count", result.RowsAffected),
			zap.Time("cutoff", cutoff),
		)
	}

	return result.RowsAffected, nil
}

// maskSensitiveFields returns a copy of the fields map with credential-related
// values replaced by "******". A field is considered sensitive if its key
// (case-insensitive) contains any of the sensitive key patterns.
func maskSensitiveFields(fields map[string]interface{}) map[string]interface{} {
	if fields == nil {
		return nil
	}

	masked := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		if isSensitiveKey(k) {
			masked[k] = "******"
		} else {
			masked[k] = v
		}
	}
	return masked
}

// isSensitiveKey checks whether a field key matches any sensitive pattern.
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, pattern := range sensitiveKeyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// HandleListAuditLogs handles the GET /api/v1/audit-logs endpoint.
// Supports pagination via page and page_size query parameters.
func (s *Service) HandleListAuditLogs(c *gin.Context) {
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

	var total int64
	s.db.Model(&domain.AuditLog{}).Count(&total)

	offset := (page - 1) * pageSize
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	var logs []domain.AuditLog
	s.db.Preload("User").Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs)

	c.JSON(http.StatusOK, gin.H{
		"logs":        logs,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}
