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

// WeChatWorkChannel implements NotificationChannel for WeChat Work (Enterprise WeChat) bot webhook.
type WeChatWorkChannel struct {
	WebhookURL string
	HTTPClient *http.Client
}

// NewWeChatWorkChannel creates a WeChatWorkChannel from a ChannelConfig.
func NewWeChatWorkChannel(config *ChannelConfig) *WeChatWorkChannel {
	return &WeChatWorkChannel{
		WebhookURL: config.GetSetting("webhook_url"),
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWeChatWorkChannelWithClient creates a WeChatWorkChannel with a custom HTTP client (for testing).
func NewWeChatWorkChannelWithClient(webhookURL string, client *http.Client) *WeChatWorkChannel {
	return &WeChatWorkChannel{
		WebhookURL: webhookURL,
		HTTPClient: client,
	}
}

// ChannelType returns "wechat_work".
func (c *WeChatWorkChannel) ChannelType() string {
	return "wechat_work"
}

// wechatMessage represents the WeChat Work bot message payload.
type wechatMessage struct {
	MsgType  string              `json:"msgtype"`
	Markdown *wechatMarkdownBody `json:"markdown,omitempty"`
	Text     *wechatTextBody     `json:"text,omitempty"`
}

type wechatMarkdownBody struct {
	Content string `json:"content"`
}

type wechatTextBody struct {
	Content string `json:"content"`
}

// wechatResponse represents the response from WeChat Work API.
type wechatResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// Send delivers a notification via WeChat Work bot webhook.
func (c *WeChatWorkChannel) Send(ctx context.Context, notification *Notification) error {
	content := fmt.Sprintf(
		"## DomainRadar Alert\n"+
			"**Severity:** %s\n"+
			"**Type:** %s\n"+
			"**Domain:** %s\n"+
			"**Time:** %s\n"+
			"**URL:** [View Domain](%s)\n\n"+
			"> %s",
		notification.Severity,
		notification.AlertType,
		notification.DomainName,
		notification.TriggeredAt.Format(time.RFC3339),
		notification.DomainURL,
		notification.Message,
	)

	msg := wechatMessage{
		MsgType: "markdown",
		Markdown: &wechatMarkdownBody{
			Content: content,
		},
	}

	return c.postMessage(ctx, &msg)
}

// TestConnection sends a test message to verify the webhook URL is valid.
func (c *WeChatWorkChannel) TestConnection(ctx context.Context, config *ChannelConfig) error {
	ch := NewWeChatWorkChannel(config)

	msg := wechatMessage{
		MsgType: "text",
		Text: &wechatTextBody{
			Content: "DomainRadar notification channel test - connection successful.",
		},
	}

	return ch.postMessage(ctx, &msg)
}

// postMessage sends a message payload to the WeChat Work bot webhook.
func (c *WeChatWorkChannel) postMessage(ctx context.Context, msg *wechatMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal wechat message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("wechat webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read wechat response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wechat webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	var result wechatResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse wechat response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("wechat webhook error (code %d): %s", result.ErrCode, result.ErrMsg)
	}

	return nil
}
