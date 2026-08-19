package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WebhookChannel tests (using httptest) ---

func TestWebhookChannel_ChannelType(t *testing.T) {
	ch := &WebhookChannel{}
	assert.Equal(t, "webhook", ch.ChannelType())
}

func TestWebhookChannel_Send_Success(t *testing.T) {
	var receivedPayload WebhookPayload
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		err = json.Unmarshal(body, &receivedPayload)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWebhookChannelWithClient(server.URL, map[string]string{
		"X-Custom-Header": "test-value",
	}, server.Client())

	triggeredAt := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	notification := &Notification{
		AlertID:     1,
		Severity:    "critical",
		AlertType:   "expiration",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Domain expires in 3 days",
		TriggeredAt: triggeredAt,
	}

	err := ch.Send(context.Background(), notification)
	require.NoError(t, err)

	// Verify payload fields match the documented schema
	assert.Equal(t, "critical", receivedPayload.AlertSeverity)
	assert.Equal(t, "expiration", receivedPayload.AlertType)
	assert.Equal(t, triggeredAt, receivedPayload.TriggeredAt)
	assert.Equal(t, "example.com", receivedPayload.DomainName)
	assert.Equal(t, "https://domainradar.local/domains/example.com", receivedPayload.DomainURL)
	assert.Equal(t, "Domain expires in 3 days", receivedPayload.Message)

	// Verify headers
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "DomainRadar-Webhook/1.0", receivedHeaders.Get("User-Agent"))
	assert.Equal(t, "test-value", receivedHeaders.Get("X-Custom-Header"))
}

func TestWebhookChannel_Send_PayloadContainsAllRequiredFields(t *testing.T) {
	var rawPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &rawPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWebhookChannelWithClient(server.URL, nil, server.Client())

	notification := &Notification{
		AlertID:     42,
		Severity:    "warning",
		AlertType:   "certificate",
		DomainName:  "test.org",
		DomainURL:   "https://domainradar.local/domains/test.org",
		Message:     "Certificate expires in 14 days",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	require.NoError(t, err)

	// Verify all required fields are present per requirement 5.6
	requiredFields := []string{"alert_severity", "alert_type", "triggered_at", "domain_name", "domain_url", "message"}
	for _, field := range requiredFields {
		_, exists := rawPayload[field]
		assert.True(t, exists, "required field %q missing from webhook payload", field)
	}
}

func TestWebhookChannel_Send_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	ch := NewWebhookChannelWithClient(server.URL, nil, server.Client())

	notification := &Notification{
		AlertID:     1,
		Severity:    "critical",
		AlertType:   "downtime",
		DomainName:  "down.example.com",
		DomainURL:   "https://domainradar.local/domains/down.example.com",
		Message:     "Website is down",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook returned status 500")
}

func TestWebhookChannel_Send_ConnectionRefused(t *testing.T) {
	ch := NewWebhookChannelWithClient("http://127.0.0.1:1", nil, &http.Client{Timeout: 1 * time.Second})

	notification := &Notification{
		AlertID:     1,
		Severity:    "critical",
		AlertType:   "downtime",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Connection test",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook request failed")
}

func TestWebhookChannel_TestConnection_Success(t *testing.T) {
	var receivedPayload WebhookPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWebhookChannelWithClient(server.URL, nil, server.Client())

	config := &ChannelConfig{Settings: map[string]string{"url": server.URL}}
	err := ch.TestConnection(context.Background(), config)
	require.NoError(t, err)

	// Test payload should have test values
	assert.Equal(t, "informational", receivedPayload.AlertSeverity)
	assert.Equal(t, "test", receivedPayload.AlertType)
	assert.Equal(t, "test.example.com", receivedPayload.DomainName)
}

func TestWebhookChannel_TestConnection_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	ch := NewWebhookChannelWithClient(server.URL, nil, server.Client())

	config := &ChannelConfig{Settings: map[string]string{"url": server.URL}}
	err := ch.TestConnection(context.Background(), config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook returned status 403")
}

func TestNewWebhookChannel_FromConfig(t *testing.T) {
	config := &ChannelConfig{
		Settings: map[string]string{
			"url":     "https://hooks.example.com/webhook",
			"headers": "Authorization:Bearer token123,X-Source:DomainRadar",
		},
	}

	ch := NewWebhookChannel(config)
	assert.Equal(t, "https://hooks.example.com/webhook", ch.TargetURL)
	assert.Equal(t, "Bearer token123", ch.Headers["Authorization"])
	assert.Equal(t, "DomainRadar", ch.Headers["X-Source"])
}

// --- WeChatWorkChannel tests (using httptest) ---

func TestWeChatWorkChannel_ChannelType(t *testing.T) {
	ch := &WeChatWorkChannel{}
	assert.Equal(t, "wechat_work", ch.ChannelType())
}

func TestWeChatWorkChannel_Send_Success(t *testing.T) {
	var receivedMsg wechatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedMsg)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 0, "errmsg": "ok"}`))
	}))
	defer server.Close()

	ch := NewWeChatWorkChannelWithClient(server.URL, server.Client())

	notification := &Notification{
		AlertID:     1,
		Severity:    "warning",
		AlertType:   "expiration",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Domain expires in 14 days",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	require.NoError(t, err)

	assert.Equal(t, "markdown", receivedMsg.MsgType)
	assert.NotNil(t, receivedMsg.Markdown)
	assert.Contains(t, receivedMsg.Markdown.Content, "example.com")
	assert.Contains(t, receivedMsg.Markdown.Content, "warning")
}

func TestWeChatWorkChannel_Send_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode": 93000, "errmsg": "invalid webhook url"}`))
	}))
	defer server.Close()

	ch := NewWeChatWorkChannelWithClient(server.URL, server.Client())

	notification := &Notification{
		AlertID:     1,
		Severity:    "critical",
		AlertType:   "downtime",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Website is down",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wechat webhook error")
}

// --- SMSChannel tests (using httptest) ---

func TestSMSChannel_ChannelType(t *testing.T) {
	ch := &SMSChannel{}
	assert.Equal(t, "sms", ch.ChannelType())
}

func TestSMSChannel_Send_Success(t *testing.T) {
	var receivedReq smsRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedReq)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	ch := NewSMSChannelWithClient(server.URL, "test-api-key", []string{"+8613800000001"}, server.Client())

	notification := &Notification{
		AlertID:     1,
		Severity:    "critical",
		AlertType:   "expiration",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Domain expires tomorrow",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	require.NoError(t, err)

	assert.Equal(t, []string{"+8613800000001"}, receivedReq.To)
	assert.Contains(t, receivedReq.Message, "CRITICAL")
	assert.Contains(t, receivedReq.Message, "example.com")
}

func TestSMSChannel_Send_NoPhoneNumbers(t *testing.T) {
	ch := NewSMSChannelWithClient("http://localhost", "key", nil, &http.Client{})

	notification := &Notification{
		AlertID:     1,
		Severity:    "warning",
		AlertType:   "expiration",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Test",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no phone numbers configured")
}

func TestSMSChannel_Send_GatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": false, "error": "insufficient balance"}`))
	}))
	defer server.Close()

	ch := NewSMSChannelWithClient(server.URL, "key", []string{"+8613800000001"}, server.Client())

	notification := &Notification{
		AlertID:     1,
		Severity:    "warning",
		AlertType:   "expiration",
		DomainName:  "example.com",
		DomainURL:   "https://domainradar.local/domains/example.com",
		Message:     "Test",
		TriggeredAt: time.Now().UTC(),
	}

	err := ch.Send(context.Background(), notification)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance")
}

// --- EmailChannel tests (basic structure only, no real SMTP) ---

func TestEmailChannel_ChannelType(t *testing.T) {
	ch := &EmailChannel{}
	assert.Equal(t, "email", ch.ChannelType())
}

func TestNewEmailChannel_FromConfig(t *testing.T) {
	config := &ChannelConfig{
		Settings: map[string]string{
			"host":     "smtp.example.com",
			"port":     "465",
			"username": "user@example.com",
			"password": "secret",
			"from":     "alerts@domainradar.local",
			"use_tls":  "true",
		},
	}

	ch := NewEmailChannel(config)
	assert.Equal(t, "smtp.example.com", ch.Host)
	assert.Equal(t, 465, ch.Port)
	assert.Equal(t, "user@example.com", ch.Username)
	assert.Equal(t, "secret", ch.Password)
	assert.Equal(t, "alerts@domainradar.local", ch.From)
	assert.True(t, ch.UseTLS)
}

func TestNewEmailChannel_DefaultPort(t *testing.T) {
	config := &ChannelConfig{
		Settings: map[string]string{
			"host": "smtp.example.com",
		},
	}

	ch := NewEmailChannel(config)
	assert.Equal(t, 587, ch.Port)
	assert.False(t, ch.UseTLS)
}

// --- NewSMSChannel from config tests ---

func TestNewSMSChannel_FromConfig(t *testing.T) {
	config := &ChannelConfig{
		Settings: map[string]string{
			"gateway_url":   "https://sms.example.com/send",
			"api_key":       "sk-12345",
			"phone_numbers": "+8613800000001, +8613800000002, +8613800000003",
		},
	}

	ch := NewSMSChannel(config)
	assert.Equal(t, "https://sms.example.com/send", ch.GatewayURL)
	assert.Equal(t, "sk-12345", ch.APIKey)
	assert.Equal(t, []string{"+8613800000001", "+8613800000002", "+8613800000003"}, ch.PhoneNumbers)
}

// --- ChannelConfig helper tests ---

func TestChannelConfig_GetSetting(t *testing.T) {
	config := &ChannelConfig{
		Settings: map[string]string{
			"key1": "value1",
		},
	}
	assert.Equal(t, "value1", config.GetSetting("key1"))
	assert.Equal(t, "", config.GetSetting("nonexistent"))
}

func TestChannelConfig_GetSetting_NilConfig(t *testing.T) {
	var config *ChannelConfig
	assert.Equal(t, "", config.GetSetting("anything"))
}

func TestChannelConfig_GetSetting_NilSettings(t *testing.T) {
	config := &ChannelConfig{}
	assert.Equal(t, "", config.GetSetting("anything"))
}

// --- Interface compliance tests ---

func TestEmailChannel_ImplementsInterface(t *testing.T) {
	var _ NotificationChannel = (*EmailChannel)(nil)
}

func TestWeChatWorkChannel_ImplementsInterface(t *testing.T) {
	var _ NotificationChannel = (*WeChatWorkChannel)(nil)
}

func TestSMSChannel_ImplementsInterface(t *testing.T) {
	var _ NotificationChannel = (*SMSChannel)(nil)
}

func TestWebhookChannel_ImplementsInterface(t *testing.T) {
	var _ NotificationChannel = (*WebhookChannel)(nil)
}
