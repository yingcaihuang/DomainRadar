package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CNEAlarmPayload is the request body for the CNE alarm push API.
type CNEAlarmPayload struct {
	AlarmCode     string `json:"alarmCode"`
	AlarmName     string `json:"alarmName"`
	AlarmValue    string `json:"alarmValue"`
	AlarmStatus   int    `json:"alarmStatus"`
	AlarmIP       string `json:"alarmIp,omitempty"`
	ExtensionInfo string `json:"extensionInfo,omitempty"`
	AlarmTime     int64  `json:"alarmTime,omitempty"`
}

// CNEAlarmChannel implements NotificationChannel for CNE alarm push API.
type CNEAlarmChannel struct {
	APIURL     string
	APIKey     string
	AlarmCode  string
	HTTPClient *http.Client
}

// NewCNEAlarmChannel creates a CNEAlarmChannel from config.
// Config keys: api_url, api_key, alarm_code (optional, default "2809")
func NewCNEAlarmChannel(config *ChannelConfig) *CNEAlarmChannel {
	apiURL := config.GetSetting("api_url")
	if apiURL == "" {
		apiURL = "https://cne.verycloud.cn/monitor/api/v1/alarms"
	}
	alarmCode := config.GetSetting("alarm_code")
	if alarmCode == "" {
		alarmCode = "2809"
	}

	return &CNEAlarmChannel{
		APIURL:     apiURL,
		APIKey:     config.GetSetting("api_key"),
		AlarmCode:  alarmCode,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ChannelType returns "cne_alarm".
func (c *CNEAlarmChannel) ChannelType() string {
	return "cne_alarm"
}

// Send delivers an alert to the CNE alarm API.
func (c *CNEAlarmChannel) Send(ctx context.Context, notification *Notification) error {
	payload := CNEAlarmPayload{
		AlarmCode:     c.AlarmCode,
		AlarmName:     notification.DomainName,
		AlarmValue:    truncate(notification.Message, 255),
		AlarmStatus:   8, // 固定严重告警 2 级
		ExtensionInfo: truncate(mapAlertTypeLabel(notification.AlertType), 128),
		AlarmTime:     notification.TriggeredAt.Unix(),
	}

	return c.postAlarm(ctx, &payload)
}

// mapAlertTypeLabel maps internal alert type to human-readable Chinese label.
func mapAlertTypeLabel(alertType string) string {
	switch alertType {
	case "expiration", "domain_expired", "domain_expiring_7d", "domain_expiring_30d":
		return "域名到期告警"
	case "certificate_error", "certificate_expired", "certificate_expiring":
		return "证书告警"
	case "whois_change":
		return "WHOIS变更告警"
	case "service_down":
		return "服务监控告警"
	case "service_recovered":
		return "服务恢复通知"
	case "email", "email_compliance":
		return "邮件安全告警"
	case "downtime":
		return "宕机告警"
	case "test":
		return "连通性测试"
	default:
		return alertType
	}
}

// TestConnection sends a test alarm to verify connectivity.
func (c *CNEAlarmChannel) TestConnection(ctx context.Context, config *ChannelConfig) error {
	ch := NewCNEAlarmChannel(config)
	payload := CNEAlarmPayload{
		AlarmCode:     ch.AlarmCode,
		AlarmName:     "DomainRadar-Test",
		AlarmValue:    "DomainRadar 告警渠道连通性测试",
		AlarmStatus:   0, // normal — just a test
		ExtensionInfo: "connectivity_test",
		AlarmTime:     time.Now().Unix(),
	}
	return ch.postAlarm(ctx, &payload)
}

func (c *CNEAlarmChannel) postAlarm(ctx context.Context, payload *CNEAlarmPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cne_alarm: failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cne_alarm: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("X-Trace-Id", fmt.Sprintf("domainradar-%d", time.Now().UnixMilli()))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cne_alarm: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("cne_alarm: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Check business code
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Code != 0 {
		return fmt.Errorf("cne_alarm: business error %d: %s", result.Code, result.Message)
	}

	return nil
}



func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
