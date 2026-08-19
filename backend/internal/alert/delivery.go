package alert

import (
	"context"
	"fmt"
	"time"

	"domainradar/internal/domain"
	"domainradar/internal/notification"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// MaxRetries is the maximum number of delivery retry attempts.
	MaxRetries = 3

	// RetryInterval is the minimum interval between retry attempts (5 minutes).
	RetryInterval = 5 * time.Minute

	// RetentionDays is the number of days to retain alert history logs.
	RetentionDays = 365
)

// AlertDeliveryService handles delivering alert notifications to configured channels
// and retrying failed deliveries.
type AlertDeliveryService struct {
	db                   *gorm.DB
	notificationChannels map[string]notification.NotificationChannel
	logger               *zap.Logger
}

// NewAlertDeliveryService creates a new AlertDeliveryService.
func NewAlertDeliveryService(db *gorm.DB, logger *zap.Logger) *AlertDeliveryService {
	return &AlertDeliveryService{
		db:                   db,
		notificationChannels: make(map[string]notification.NotificationChannel),
		logger:               logger,
	}
}

// RegisterChannel registers a notification channel implementation by its type name.
func (s *AlertDeliveryService) RegisterChannel(channelType string, channel notification.NotificationChannel) {
	s.notificationChannels[channelType] = channel
}

// DeliverAlert sends notifications for an alert to all users assigned to the domain
// via configured channels based on notification rules.
func (s *AlertDeliveryService) DeliverAlert(ctx context.Context, alert *domain.Alert) error {
	s.logger.Info("delivering alert",
		zap.Uint("alert_id", alert.ID),
		zap.Uint("domain_id", alert.DomainID),
		zap.String("severity", alert.Severity))

	// Find notification rules matching this alert's domain and severity
	var rules []domain.NotificationRule
	if err := s.db.WithContext(ctx).
		Preload("Channel").
		Where("domain_id = ? AND severity_filter = ?", alert.DomainID, alert.Severity).
		Find(&rules).Error; err != nil {
		s.logger.Error("failed to find notification rules",
			zap.Uint("alert_id", alert.ID),
			zap.Error(err))
		return fmt.Errorf("finding notification rules: %w", err)
	}

	if len(rules) == 0 {
		s.logger.Info("no notification rules configured for alert",
			zap.Uint("alert_id", alert.ID),
			zap.String("severity", alert.Severity))
		// Update delivery status to delivered (no channels to notify is not a failure)
		s.db.WithContext(ctx).Model(alert).Update("delivery_status", "delivered")
		return nil
	}

	// Load the domain for notification context
	var dom domain.NormalizedDomain
	if err := s.db.WithContext(ctx).First(&dom, alert.DomainID).Error; err != nil {
		s.logger.Error("failed to load domain for alert delivery",
			zap.Uint("domain_id", alert.DomainID),
			zap.Error(err))
		return fmt.Errorf("loading domain: %w", err)
	}

	// Deliver to each matching channel
	var anySuccess bool
	for _, rule := range rules {
		channelImpl, ok := s.notificationChannels[rule.Channel.ChannelType]
		if !ok {
			s.logger.Warn("no channel implementation registered",
				zap.String("channel_type", rule.Channel.ChannelType),
				zap.Uint("channel_id", rule.ChannelID))
			s.createNotificationLog(ctx, alert.ID, rule.ChannelID, "failed", "no channel implementation registered", 0)
			continue
		}

		// Build notification payload
		notif := &notification.Notification{
			AlertID:     alert.ID,
			Severity:    alert.Severity,
			AlertType:   alert.AlertType,
			DomainName:  dom.DomainName,
			DomainURL:   fmt.Sprintf("/domains/%d", dom.ID),
			Message:     alert.Message,
			TriggeredAt: alert.GeneratedAt,
		}

		// Attempt to send
		err := channelImpl.Send(ctx, notif)
		now := time.Now()

		if err != nil {
			s.logger.Warn("notification delivery failed",
				zap.Uint("alert_id", alert.ID),
				zap.Uint("channel_id", rule.ChannelID),
				zap.String("channel_type", rule.Channel.ChannelType),
				zap.Error(err))
			s.createNotificationLog(ctx, alert.ID, rule.ChannelID, "failed", err.Error(), 0)
		} else {
			s.logger.Info("notification delivered",
				zap.Uint("alert_id", alert.ID),
				zap.Uint("channel_id", rule.ChannelID),
				zap.String("channel_type", rule.Channel.ChannelType))
			s.createNotificationLogWithSentAt(ctx, alert.ID, rule.ChannelID, "sent", "", 0, &now)
			anySuccess = true
		}
	}

	// Update alert delivery status
	if anySuccess {
		s.db.WithContext(ctx).Model(alert).Update("delivery_status", "delivered")
	} else {
		s.db.WithContext(ctx).Model(alert).Update("delivery_status", "failed")
	}

	return nil
}

// RetryFailedDeliveries finds notification logs with status "failed" and retry_count < MaxRetries,
// respects the 5-minute interval, and retries delivery. Marks as permanently failed after exhausting retries.
func (s *AlertDeliveryService) RetryFailedDeliveries(ctx context.Context) error {
	s.logger.Info("starting retry of failed deliveries")

	// Find failed notification logs eligible for retry
	var failedLogs []domain.NotificationLog
	cutoff := time.Now().Add(-RetryInterval)

	if err := s.db.WithContext(ctx).
		Preload("Alert").
		Preload("Channel").
		Where("status = ? AND retry_count < ? AND created_at <= ?", "failed", MaxRetries, cutoff).
		Find(&failedLogs).Error; err != nil {
		s.logger.Error("failed to find failed notification logs", zap.Error(err))
		return fmt.Errorf("finding failed logs: %w", err)
	}

	s.logger.Info("found failed deliveries for retry", zap.Int("count", len(failedLogs)))

	for _, log := range failedLogs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if enough time has passed since last attempt (5-minute interval)
		if !s.isRetryEligible(log) {
			continue
		}

		// Get channel implementation
		channelImpl, ok := s.notificationChannels[log.Channel.ChannelType]
		if !ok {
			s.logger.Warn("no channel implementation for retry",
				zap.String("channel_type", log.Channel.ChannelType),
				zap.Uint("log_id", log.ID))
			s.markPermanentlyFailed(ctx, &log)
			continue
		}

		// Load the domain for the alert
		var dom domain.NormalizedDomain
		if err := s.db.WithContext(ctx).First(&dom, log.Alert.DomainID).Error; err != nil {
			s.logger.Error("failed to load domain for retry",
				zap.Uint("domain_id", log.Alert.DomainID),
				zap.Error(err))
			continue
		}

		// Build notification and attempt delivery
		notif := &notification.Notification{
			AlertID:     log.AlertID,
			Severity:    log.Alert.Severity,
			AlertType:   log.Alert.AlertType,
			DomainName:  dom.DomainName,
			DomainURL:   fmt.Sprintf("/domains/%d", dom.ID),
			Message:     log.Alert.Message,
			TriggeredAt: log.Alert.GeneratedAt,
		}

		err := channelImpl.Send(ctx, notif)
		newRetryCount := log.RetryCount + 1
		now := time.Now()

		if err != nil {
			s.logger.Warn("retry delivery failed",
				zap.Uint("log_id", log.ID),
				zap.Int("retry_count", newRetryCount),
				zap.Error(err))

			if newRetryCount >= MaxRetries {
				// All retries exhausted - mark as permanently failed
				s.db.WithContext(ctx).Model(&log).Updates(map[string]interface{}{
					"status":       "failed",
					"retry_count":  newRetryCount,
					"error_reason": err.Error(),
					"created_at":   now, // update timestamp for interval tracking
				})
				s.updateAlertDeliveryStatusIfAllFailed(ctx, log.AlertID)
			} else {
				// Update retry count and timestamp
				s.db.WithContext(ctx).Model(&log).Updates(map[string]interface{}{
					"retry_count":  newRetryCount,
					"error_reason": err.Error(),
					"created_at":   now, // update timestamp for interval tracking
				})
			}
		} else {
			// Retry succeeded
			s.logger.Info("retry delivery succeeded",
				zap.Uint("log_id", log.ID),
				zap.Int("retry_count", newRetryCount))

			s.db.WithContext(ctx).Model(&log).Updates(map[string]interface{}{
				"status":      "sent",
				"retry_count": newRetryCount,
				"sent_at":     &now,
			})

			// Update alert delivery status to delivered
			s.db.WithContext(ctx).Model(&domain.Alert{}).
				Where("id = ?", log.AlertID).
				Update("delivery_status", "delivered")
		}
	}

	return nil
}

// CleanupOldLogs removes notification logs older than RetentionDays (365 days).
func (s *AlertDeliveryService) CleanupOldLogs(ctx context.Context) error {
	cutoff := time.Now().AddDate(0, 0, -RetentionDays)
	result := s.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&domain.NotificationLog{})
	if result.Error != nil {
		return fmt.Errorf("cleaning up old notification logs: %w", result.Error)
	}
	s.logger.Info("cleaned up old notification logs",
		zap.Int64("deleted", result.RowsAffected))
	return nil
}

// isRetryEligible checks if the minimum retry interval has elapsed since the last attempt.
func (s *AlertDeliveryService) isRetryEligible(log domain.NotificationLog) bool {
	return time.Since(log.CreatedAt) >= RetryInterval
}

// markPermanentlyFailed sets a notification log to permanently failed state.
func (s *AlertDeliveryService) markPermanentlyFailed(ctx context.Context, log *domain.NotificationLog) {
	s.db.WithContext(ctx).Model(log).Updates(map[string]interface{}{
		"status":       "failed",
		"retry_count":  MaxRetries,
		"error_reason": "channel implementation not available",
	})
	s.updateAlertDeliveryStatusIfAllFailed(ctx, log.AlertID)
}

// updateAlertDeliveryStatusIfAllFailed checks if all notification logs for an alert have failed,
// and if so, marks the alert's DeliveryStatus as "failed".
func (s *AlertDeliveryService) updateAlertDeliveryStatusIfAllFailed(ctx context.Context, alertID uint) {
	var totalLogs int64
	var failedLogs int64

	s.db.WithContext(ctx).Model(&domain.NotificationLog{}).
		Where("alert_id = ?", alertID).
		Count(&totalLogs)

	s.db.WithContext(ctx).Model(&domain.NotificationLog{}).
		Where("alert_id = ? AND status = ? AND retry_count >= ?", alertID, "failed", MaxRetries).
		Count(&failedLogs)

	if totalLogs > 0 && failedLogs == totalLogs {
		s.db.WithContext(ctx).Model(&domain.Alert{}).
			Where("id = ?", alertID).
			Update("delivery_status", "failed")
	}
}

// createNotificationLog creates a notification log entry with the given parameters.
func (s *AlertDeliveryService) createNotificationLog(ctx context.Context, alertID, channelID uint, status, errorReason string, retryCount int) {
	s.createNotificationLogWithSentAt(ctx, alertID, channelID, status, errorReason, retryCount, nil)
}

// createNotificationLogWithSentAt creates a notification log entry with optional sent timestamp.
func (s *AlertDeliveryService) createNotificationLogWithSentAt(ctx context.Context, alertID, channelID uint, status, errorReason string, retryCount int, sentAt *time.Time) {
	log := domain.NotificationLog{
		AlertID:     alertID,
		ChannelID:   channelID,
		Status:      status,
		ErrorReason: errorReason,
		RetryCount:  retryCount,
		SentAt:      sentAt,
		CreatedAt:   time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(&log).Error; err != nil {
		s.logger.Error("failed to create notification log",
			zap.Uint("alert_id", alertID),
			zap.Uint("channel_id", channelID),
			zap.Error(err))
	}
}
