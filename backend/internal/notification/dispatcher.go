package notification

import (
	"context"
	"fmt"
	"math"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// MaxRetries is the maximum number of delivery retries per channel.
	MaxRetries = 3

	// BaseBackoff is the initial backoff duration for retry (5 seconds).
	BaseBackoff = 5 * time.Second

	// DeliveryTimeout is the max time allowed for a single channel delivery.
	DeliveryTimeout = 30 * time.Second

	// CriticalReattemptDelay is the delay before reattempting a critical undelivered alert.
	CriticalReattemptDelay = 5 * time.Minute

	// StatusSent indicates successful delivery.
	StatusSent = "sent"

	// StatusFailed indicates all retries exhausted.
	StatusFailed = "failed"

	// StatusRetrying indicates the notification is being retried.
	StatusRetrying = "retrying"

	// DeliveryStatusUndelivered flags a critical alert where all channels failed.
	DeliveryStatusUndelivered = "undelivered"

	// DeliveryStatusDelivered marks a successfully delivered alert.
	DeliveryStatusDelivered = "delivered"
)

// Dispatcher handles routing notifications to configured channels with retry logic.
type Dispatcher struct {
	db       *gorm.DB
	channels map[uint]NotificationChannel
	logger   *zap.Logger
}

// NewDispatcher creates a new Dispatcher instance.
func NewDispatcher(db *gorm.DB, logger *zap.Logger) *Dispatcher {
	return &Dispatcher{
		db:       db,
		channels: make(map[uint]NotificationChannel),
		logger:   logger,
	}
}

// RegisterChannel registers a channel implementation for a given channel ID.
func (d *Dispatcher) RegisterChannel(channelID uint, channel NotificationChannel) {
	d.channels[channelID] = channel
}

// DispatchAlert routes an alert notification to all channels configured for the alert's severity.
// It initiates delivery within 30 seconds of being called and records results as NotificationLogs.
func (d *Dispatcher) DispatchAlert(ctx context.Context, alert *domain.Alert) error {
	// Find notification rules matching the alert's severity
	var rules []domain.NotificationRule
	if err := d.db.WithContext(ctx).
		Where("severity_filter = ?", alert.Severity).
		Find(&rules).Error; err != nil {
		d.logger.Error("failed to find notification rules",
			zap.Uint("alert_id", alert.ID),
			zap.String("severity", alert.Severity),
			zap.Error(err))
		return fmt.Errorf("finding notification rules: %w", err)
	}

	if len(rules) == 0 {
		d.logger.Info("no notification rules configured for severity",
			zap.Uint("alert_id", alert.ID),
			zap.String("severity", alert.Severity))
		return nil
	}

	// Build notification payload
	notification := d.buildNotification(alert)

	allFailed := true
	for _, rule := range rules {
		channel, ok := d.channels[rule.ChannelID]
		if !ok {
			d.logger.Warn("channel not registered",
				zap.Uint("channel_id", rule.ChannelID),
				zap.Uint("alert_id", alert.ID))
			d.recordFailure(ctx, alert.ID, rule.ChannelID, 0, "channel not registered")
			continue
		}

		// Attempt delivery with timeout
		err := d.deliverWithTimeout(ctx, channel, notification)
		now := time.Now()
		if err == nil {
			allFailed = false
			// Record successful delivery
			log := domain.NotificationLog{
				AlertID:   alert.ID,
				ChannelID: rule.ChannelID,
				Status:    StatusSent,
				SentAt:    &now,
				CreatedAt: now,
			}
			if dbErr := d.db.WithContext(ctx).Create(&log).Error; dbErr != nil {
				d.logger.Error("failed to record notification log",
					zap.Uint("alert_id", alert.ID),
					zap.Error(dbErr))
			}
			d.logger.Info("notification delivered",
				zap.Uint("alert_id", alert.ID),
				zap.Uint("channel_id", rule.ChannelID))
		} else {
			// Record initial failure with retry_count = 0
			d.recordFailure(ctx, alert.ID, rule.ChannelID, 0, err.Error())
			d.logger.Warn("notification delivery failed",
				zap.Uint("alert_id", alert.ID),
				zap.Uint("channel_id", rule.ChannelID),
				zap.Error(err))
		}
	}

	// If all channels failed for a critical alert, flag as undelivered
	if allFailed && len(rules) > 0 && alert.Severity == "critical" {
		if err := d.db.WithContext(ctx).
			Model(&domain.Alert{}).
			Where("id = ?", alert.ID).
			Update("delivery_status", DeliveryStatusUndelivered).Error; err != nil {
			d.logger.Error("failed to flag alert as undelivered",
				zap.Uint("alert_id", alert.ID),
				zap.Error(err))
		}
	} else if !allFailed {
		// Mark alert as delivered if at least one channel succeeded
		if err := d.db.WithContext(ctx).
			Model(&domain.Alert{}).
			Where("id = ?", alert.ID).
			Update("delivery_status", DeliveryStatusDelivered).Error; err != nil {
			d.logger.Error("failed to update alert delivery status",
				zap.Uint("alert_id", alert.ID),
				zap.Error(err))
		}
	}

	return nil
}

// CalculateNotificationBackoff returns the backoff duration for a given retry attempt.
// Formula: 5s * 2^retryCount → Retry 0: 5s, Retry 1: 10s, Retry 2: 20s
func CalculateNotificationBackoff(retryCount int) time.Duration {
	return BaseBackoff * time.Duration(math.Pow(2, float64(retryCount)))
}

// RetryFailedNotifications finds failed notification logs with retry_count < MaxRetries
// and reattempts delivery with exponential backoff. For critical alerts flagged as
// "undelivered", it checks that at least 5 minutes have elapsed before reattempting.
func (d *Dispatcher) RetryFailedNotifications(ctx context.Context) error {
	var failedLogs []domain.NotificationLog
	if err := d.db.WithContext(ctx).
		Preload("Alert").
		Where("status = ? AND retry_count < ?", StatusFailed, MaxRetries).
		Find(&failedLogs).Error; err != nil {
		d.logger.Error("failed to query failed notification logs", zap.Error(err))
		return fmt.Errorf("querying failed logs: %w", err)
	}

	d.logger.Info("retrying failed notifications", zap.Int("count", len(failedLogs)))

	for _, log := range failedLogs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check backoff timing - ensure enough time has passed since last attempt
		backoff := CalculateNotificationBackoff(log.RetryCount)
		earliestRetry := log.CreatedAt.Add(backoff)
		if log.SentAt != nil {
			earliestRetry = log.SentAt.Add(backoff)
		}
		if time.Now().Before(earliestRetry) {
			continue
		}

		// For critical undelivered alerts, enforce 5-minute minimum between reattempts
		if log.Alert.Severity == "critical" && log.Alert.DeliveryStatus == DeliveryStatusUndelivered {
			lastAttemptTime := log.CreatedAt
			if log.SentAt != nil {
				lastAttemptTime = *log.SentAt
			}
			if time.Since(lastAttemptTime) < CriticalReattemptDelay {
				continue
			}
		}

		channel, ok := d.channels[log.ChannelID]
		if !ok {
			d.logger.Warn("channel not registered for retry",
				zap.Uint("channel_id", log.ChannelID))
			continue
		}

		// Build notification from alert
		notification := d.buildNotificationFromAlert(&log.Alert)

		// Attempt delivery
		err := d.deliverWithTimeout(ctx, channel, notification)
		now := time.Now()

		if err == nil {
			// Update log as sent
			d.db.WithContext(ctx).Model(&log).Updates(map[string]interface{}{
				"status":      StatusSent,
				"retry_count": log.RetryCount + 1,
				"sent_at":     now,
			})

			// Update alert delivery status
			d.db.WithContext(ctx).Model(&domain.Alert{}).
				Where("id = ?", log.AlertID).
				Update("delivery_status", DeliveryStatusDelivered)

			d.logger.Info("retry delivery succeeded",
				zap.Uint("alert_id", log.AlertID),
				zap.Uint("channel_id", log.ChannelID),
				zap.Int("retry_count", log.RetryCount+1))
		} else {
			newRetryCount := log.RetryCount + 1
			newStatus := StatusFailed
			if newRetryCount >= MaxRetries {
				newStatus = StatusFailed
				// If all retries exhausted and critical, flag alert as undelivered
				if log.Alert.Severity == "critical" {
					d.flagCriticalUndelivered(ctx, log.AlertID)
				}
			}

			d.db.WithContext(ctx).Model(&log).Updates(map[string]interface{}{
				"status":       newStatus,
				"retry_count":  newRetryCount,
				"error_reason": err.Error(),
				"sent_at":      now,
			})

			d.logger.Warn("retry delivery failed",
				zap.Uint("alert_id", log.AlertID),
				zap.Uint("channel_id", log.ChannelID),
				zap.Int("retry_count", newRetryCount),
				zap.Error(err))
		}
	}

	return nil
}

// deliverWithTimeout sends a notification to a channel with a 30-second timeout.
func (d *Dispatcher) deliverWithTimeout(ctx context.Context, channel NotificationChannel, notification *Notification) error {
	deliveryCtx, cancel := context.WithTimeout(ctx, DeliveryTimeout)
	defer cancel()

	return channel.Send(deliveryCtx, notification)
}

// recordFailure creates a NotificationLog entry recording a delivery failure.
func (d *Dispatcher) recordFailure(ctx context.Context, alertID, channelID uint, retryCount int, errorReason string) {
	now := time.Now()
	log := domain.NotificationLog{
		AlertID:     alertID,
		ChannelID:   channelID,
		Status:      StatusFailed,
		ErrorReason: errorReason,
		RetryCount:  retryCount,
		SentAt:      &now,
		CreatedAt:   now,
	}
	if err := d.db.WithContext(ctx).Create(&log).Error; err != nil {
		d.logger.Error("failed to record failure log",
			zap.Uint("alert_id", alertID),
			zap.Uint("channel_id", channelID),
			zap.Error(err))
	}
}

// flagCriticalUndelivered marks a critical alert as undelivered in the dashboard.
func (d *Dispatcher) flagCriticalUndelivered(ctx context.Context, alertID uint) {
	if err := d.db.WithContext(ctx).
		Model(&domain.Alert{}).
		Where("id = ?", alertID).
		Update("delivery_status", DeliveryStatusUndelivered).Error; err != nil {
		d.logger.Error("failed to flag critical alert as undelivered",
			zap.Uint("alert_id", alertID),
			zap.Error(err))
	}
}

// buildNotification creates a Notification payload from an Alert.
func (d *Dispatcher) buildNotification(alert *domain.Alert) *Notification {
	return &Notification{
		AlertID:     alert.ID,
		Severity:    alert.Severity,
		AlertType:   alert.AlertType,
		DomainName:  alert.Domain.DomainName,
		DomainURL:   fmt.Sprintf("/domains/%d", alert.DomainID),
		Message:     alert.Message,
		TriggeredAt: alert.GeneratedAt,
	}
}

// buildNotificationFromAlert creates a Notification from a preloaded Alert.
func (d *Dispatcher) buildNotificationFromAlert(alert *domain.Alert) *Notification {
	return &Notification{
		AlertID:     alert.ID,
		Severity:    alert.Severity,
		AlertType:   alert.AlertType,
		DomainName:  alert.Domain.DomainName,
		DomainURL:   fmt.Sprintf("/domains/%d", alert.DomainID),
		Message:     alert.Message,
		TriggeredAt: alert.GeneratedAt,
	}
}
