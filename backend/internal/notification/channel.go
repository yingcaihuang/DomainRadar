package notification

import (
	"context"
	"time"
)

// NotificationChannel is the interface for all delivery channels.
type NotificationChannel interface {
	// Send delivers a notification through the channel.
	Send(ctx context.Context, notification *Notification) error

	// TestConnection validates connectivity and credentials for the channel.
	TestConnection(ctx context.Context, config *ChannelConfig) error

	// ChannelType returns the identifier for this channel (e.g., "email", "wechat_work", "sms", "webhook").
	ChannelType() string
}

// Notification contains the alert data to be sent through a channel.
type Notification struct {
	AlertID     uint      `json:"alert_id"`
	Severity    string    `json:"severity"`     // "informational", "warning", "critical", "expired"
	AlertType   string    `json:"alert_type"`   // "expiration", "certificate", "downtime", "email", "dns"
	DomainName  string    `json:"domain_name"`
	DomainURL   string    `json:"domain_url"`
	Message     string    `json:"message"`
	TriggeredAt time.Time `json:"triggered_at"`
}

// ChannelConfig holds channel-specific configuration as a flexible key-value map.
// Each channel type interprets its own expected keys (e.g., "host", "port" for email).
type ChannelConfig struct {
	Settings map[string]string `json:"settings"`
}

// GetSetting returns the value for a key, or empty string if not present.
func (c *ChannelConfig) GetSetting(key string) string {
	if c == nil || c.Settings == nil {
		return ""
	}
	return c.Settings[key]
}
