package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebhookPayload is the documented JSON schema for webhook notifications.
// It conforms to requirement 5.6: alert_severity, alert_type, triggered_at,
// domain_name, domain_url, message.
type WebhookPayload struct {
	AlertSeverity string    `json:"alert_severity"`
	AlertType     string    `json:"alert_type"`
	TriggeredAt   time.Time `json:"triggered_at"`
	DomainName    string    `json:"domain_name"`
	DomainURL     string    `json:"domain_url"`
	Message       string    `json:"message"`
}

// WebhookChannel implements NotificationChannel for generic webhook delivery.
type WebhookChannel struct {
	TargetURL  string
	Headers    map[string]string
	HTTPClient *http.Client
}

// NewWebhookChannel creates a WebhookChannel from a ChannelConfig.
func NewWebhookChannel(config *ChannelConfig) *WebhookChannel {
	headers := make(map[string]string)

	// Parse headers from config (format: "Key1:Value1,Key2:Value2")
	if headerStr := config.GetSetting("headers"); headerStr != "" {
		for _, h := range strings.Split(headerStr, ",") {
			parts := strings.SplitN(strings.TrimSpace(h), ":", 2)
			if len(parts) == 2 {
				headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	return &WebhookChannel{
		TargetURL:  config.GetSetting("url"),
		Headers:    headers,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWebhookChannelWithClient creates a WebhookChannel with a custom HTTP client (for testing).
func NewWebhookChannelWithClient(targetURL string, headers map[string]string, client *http.Client) *WebhookChannel {
	if headers == nil {
		headers = make(map[string]string)
	}
	return &WebhookChannel{
		TargetURL:  targetURL,
		Headers:    headers,
		HTTPClient: client,
	}
}

// ChannelType returns "webhook".
func (c *WebhookChannel) ChannelType() string {
	return "webhook"
}

// Send delivers a notification as a JSON webhook payload.
func (c *WebhookChannel) Send(ctx context.Context, notification *Notification) error {
	payload := WebhookPayload{
		AlertSeverity: notification.Severity,
		AlertType:     notification.AlertType,
		TriggeredAt:   notification.TriggeredAt,
		DomainName:    notification.DomainName,
		DomainURL:     notification.DomainURL,
		Message:       notification.Message,
	}

	return c.postPayload(ctx, c.TargetURL, c.Headers, &payload)
}

// TestConnection sends a test payload to verify webhook connectivity.
func (c *WebhookChannel) TestConnection(ctx context.Context, config *ChannelConfig) error {
	ch := NewWebhookChannel(config)

	testPayload := WebhookPayload{
		AlertSeverity: "informational",
		AlertType:     "test",
		TriggeredAt:   time.Now().UTC(),
		DomainName:    "test.example.com",
		DomainURL:     "https://domainradar.local/domains/test.example.com",
		Message:       "DomainRadar webhook notification test - connection successful.",
	}

	return ch.postPayload(ctx, ch.TargetURL, ch.Headers, &testPayload)
}

// postPayload marshals and sends a JSON payload to the target URL.
func (c *WebhookChannel) postPayload(ctx context.Context, url string, headers map[string]string, payload *WebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DomainRadar-Webhook/1.0")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error reporting
	respBody, _ := io.ReadAll(resp.Body)

	// Accept 2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
