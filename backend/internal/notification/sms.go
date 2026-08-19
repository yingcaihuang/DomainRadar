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

// SMSChannel implements NotificationChannel for SMS gateway delivery.
type SMSChannel struct {
	GatewayURL   string
	APIKey       string
	PhoneNumbers []string
	HTTPClient   *http.Client
}

// NewSMSChannel creates an SMSChannel from a ChannelConfig.
func NewSMSChannel(config *ChannelConfig) *SMSChannel {
	phones := strings.Split(config.GetSetting("phone_numbers"), ",")
	cleaned := make([]string, 0, len(phones))
	for _, p := range phones {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}

	return &SMSChannel{
		GatewayURL:   config.GetSetting("gateway_url"),
		APIKey:       config.GetSetting("api_key"),
		PhoneNumbers: cleaned,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// NewSMSChannelWithClient creates an SMSChannel with a custom HTTP client (for testing).
func NewSMSChannelWithClient(gatewayURL, apiKey string, phones []string, client *http.Client) *SMSChannel {
	return &SMSChannel{
		GatewayURL:   gatewayURL,
		APIKey:       apiKey,
		PhoneNumbers: phones,
		HTTPClient:   client,
	}
}

// ChannelType returns "sms".
func (c *SMSChannel) ChannelType() string {
	return "sms"
}

// smsRequest represents the payload sent to the SMS gateway.
type smsRequest struct {
	To      []string `json:"to"`
	Message string   `json:"message"`
}

// smsResponse represents the response from the SMS gateway.
type smsResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Send delivers a notification via SMS gateway.
func (c *SMSChannel) Send(ctx context.Context, notification *Notification) error {
	if len(c.PhoneNumbers) == 0 {
		return fmt.Errorf("no phone numbers configured for SMS channel")
	}

	message := fmt.Sprintf(
		"[DomainRadar %s] %s - %s: %s",
		strings.ToUpper(notification.Severity),
		notification.AlertType,
		notification.DomainName,
		notification.Message,
	)

	payload := smsRequest{
		To:      c.PhoneNumbers,
		Message: message,
	}

	return c.sendSMS(ctx, &payload)
}

// TestConnection sends a test SMS to verify gateway connectivity.
func (c *SMSChannel) TestConnection(ctx context.Context, config *ChannelConfig) error {
	ch := NewSMSChannel(config)

	if len(ch.PhoneNumbers) == 0 {
		return fmt.Errorf("no phone numbers configured for SMS test")
	}

	payload := smsRequest{
		To:      ch.PhoneNumbers[:1], // Send test to first number only
		Message: "DomainRadar SMS notification test - connection successful.",
	}

	return ch.sendSMS(ctx, &payload)
}

// sendSMS posts the SMS payload to the gateway.
func (c *SMSChannel) sendSMS(ctx context.Context, payload *smsRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal SMS payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GatewayURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create SMS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("SMS gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read SMS gateway response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SMS gateway returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result smsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse SMS response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("SMS gateway error: %s", result.Error)
	}

	return nil
}
