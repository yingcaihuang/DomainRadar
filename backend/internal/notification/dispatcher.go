package notification

import (
	"context"
	"encoding/json"
	"time"

	"domainradar/internal/domain"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AlertDispatcher sends webhook notifications when alerts are created.
type AlertDispatcher struct {
	db              *gorm.DB
	cryptoService   interface{ Decrypt(string) (string, error) }
	channelRegistry *ChannelRegistry
	logger          *zap.Logger
}

// NewAlertDispatcher creates a new AlertDispatcher.
func NewAlertDispatcher(db *gorm.DB, cryptoService interface{ Decrypt(string) (string, error) }, channelRegistry *ChannelRegistry, logger *zap.Logger) *AlertDispatcher {
	return &AlertDispatcher{db: db, cryptoService: cryptoService, channelRegistry: channelRegistry, logger: logger}
}

// DispatchAlert sends the alert to all active webhook channels.
func (d *AlertDispatcher) DispatchAlert(alert *domain.Alert) {
	// Get domain name
	var domainObj domain.NormalizedDomain
	if alert.DomainID != nil {
		if err := d.db.Select("domain_name").First(&domainObj, *alert.DomainID).Error; err != nil {
			d.logger.Error("Failed to load domain for notification",
				zap.Uint("alert_id", alert.ID), zap.Error(err))
			return
		}
	}

	// Get all active channels
	var channels []domain.NotificationChannel
	if err := d.db.Where("status = ?", "active").Find(&channels).Error; err != nil {
		d.logger.Error("Failed to fetch notification channels", zap.Error(err))
		return
	}

	if len(channels) == 0 {
		return
	}

	for _, ch := range channels {
		go func(channel domain.NotificationChannel) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			// Decrypt config
			config := &ChannelConfig{Settings: map[string]string{}}
			if channel.ConfigEncrypted != "" && d.cryptoService != nil {
				plaintext, err := d.cryptoService.Decrypt(channel.ConfigEncrypted)
				if err != nil {
					d.logger.Error("Failed to decrypt channel config", zap.Uint("channel_id", channel.ID), zap.Error(err))
					return
				}
				var settings map[string]string
				if err := json.Unmarshal([]byte(plaintext), &settings); err != nil {
					d.logger.Error("Failed to parse channel config", zap.Uint("channel_id", channel.ID), zap.Error(err))
					return
				}
				config.Settings = settings
			}

			// Get factory and create channel instance
			factory, err := d.channelRegistry.Get(channel.ChannelType)
			if err != nil {
				return
			}

			notifChannel := factory(config)

			// Build notification and send
			notif := &Notification{
				AlertID:     alert.ID,
				Severity:    alert.Severity,
				AlertType:   alert.AlertType,
				DomainName:  domainObj.DomainName,
				Message:     alert.Message,
				TriggeredAt: alert.GeneratedAt,
			}

			sendErr := notifChannel.Send(ctx, notif)
			now := time.Now()

			if sendErr != nil {
				d.logger.Error("Failed to send notification",
					zap.Uint("channel_id", channel.ID),
					zap.String("channel_type", channel.ChannelType),
					zap.String("alert_type", alert.AlertType),
					zap.Error(sendErr),
				)
				// Record failure
				d.db.Create(&domain.NotificationLog{
					AlertID:     alert.ID,
					ChannelID:   channel.ID,
					Status:      "failed",
					ErrorReason: sendErr.Error(),
					CreatedAt:   now,
				})
			} else {
				d.logger.Info("Notification sent",
					zap.Uint("channel_id", channel.ID),
					zap.String("channel_type", channel.ChannelType),
					zap.String("domain", domainObj.DomainName),
					zap.String("alert_type", alert.AlertType),
				)
				// Record success
				d.db.Create(&domain.NotificationLog{
					AlertID:   alert.ID,
					ChannelID: channel.ID,
					Status:    "sent",
					SentAt:    &now,
					CreatedAt: now,
				})
			}
		}(ch)
	}
}
