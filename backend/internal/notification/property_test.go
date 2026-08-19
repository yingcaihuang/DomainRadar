package notification

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"pgregory.net/rapid"
)

// setupPropertyTestDB creates an in-memory SQLite DB for property tests.
func setupPropertyTestDB(t *rapid.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	if err := db.AutoMigrate(
		&domain.NormalizedDomain{},
		&domain.Alert{},
		&domain.NotificationChannel{},
		&domain.NotificationRule{},
		&domain.NotificationLog{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return db
}

// setupPropertyTestDBStd creates an in-memory SQLite DB for standard testing.T.
func setupPropertyTestDBStd(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&domain.NormalizedDomain{},
		&domain.Alert{},
		&domain.NotificationChannel{},
		&domain.NotificationRule{},
		&domain.NotificationLog{},
	)
	require.NoError(t, err)

	return db
}

// trackingChannel records which notifications it received for verification.
type trackingChannel struct {
	channelID uint
	sent      []*Notification
}

func newTrackingChannel(id uint) *trackingChannel {
	return &trackingChannel{channelID: id, sent: make([]*Notification, 0)}
}

func (c *trackingChannel) Send(_ context.Context, notification *Notification) error {
	c.sent = append(c.sent, notification)
	return nil
}

func (c *trackingChannel) TestConnection(_ context.Context, _ *ChannelConfig) error {
	return nil
}

func (c *trackingChannel) ChannelType() string {
	return "tracking"
}

// TestProperty10_NotificationDeliveryExponentialBackoff verifies that
// CalculateNotificationBackoff returns 5×2^N seconds for retry N.
//
// **Validates: Requirements 5.2**
func TestProperty10_NotificationDeliveryExponentialBackoff(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random retry count in range [0, 10] to stay in reasonable bounds
		retryCount := rapid.IntRange(0, 10).Draw(t, "retryCount")

		backoff := CalculateNotificationBackoff(retryCount)

		// Expected: 5 * 2^retryCount seconds
		expectedSeconds := 5.0 * math.Pow(2, float64(retryCount))
		expected := time.Duration(expectedSeconds) * time.Second

		if backoff != expected {
			t.Fatalf("CalculateNotificationBackoff(%d) = %v, want %v",
				retryCount, backoff, expected)
		}

		// Additional property: backoff is strictly increasing with retryCount
		if retryCount > 0 {
			prevBackoff := CalculateNotificationBackoff(retryCount - 1)
			if backoff <= prevBackoff {
				t.Fatalf("backoff not strictly increasing: retry %d (%v) <= retry %d (%v)",
					retryCount, backoff, retryCount-1, prevBackoff)
			}
		}

		// Additional property: backoff is always a multiple of BaseBackoff (5s)
		if backoff%BaseBackoff != 0 {
			t.Fatalf("backoff %v is not a multiple of BaseBackoff (%v)", backoff, BaseBackoff)
		}
	})
}

// TestProperty21_NotificationSeverityToChannelRouting verifies that when an alert
// is dispatched, notifications are attempted for exactly those channels whose
// rules match the alert's severity.
//
// **Validates: Requirements 5.4, 5.5**
func TestProperty21_NotificationSeverityToChannelRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		db := setupPropertyTestDB(t)
		zapLogger, _ := zap.NewDevelopment()

		// Generate random severity for the alert
		severities := []string{"informational", "warning", "critical", "expired"}
		alertSeverityIdx := rapid.IntRange(0, len(severities)-1).Draw(t, "alertSeverityIdx")
		alertSeverity := severities[alertSeverityIdx]

		// Generate a random number of channels (1 to 5)
		numChannels := rapid.IntRange(1, 5).Draw(t, "numChannels")

		// Create domain
		dom := &domain.NormalizedDomain{
			DomainName:     "test-property21.example.com",
			DataSourceType: "manual",
			Status:         "active",
		}
		if err := db.Create(dom).Error; err != nil {
			t.Fatalf("failed to create domain: %v", err)
		}

		// Create alert with the random severity
		alert := &domain.Alert{
			DomainID:       dom.ID,
			AlertType:      "expiration",
			Severity:       alertSeverity,
			Message:        "Property test alert",
			DeliveryStatus: "pending",
			GeneratedAt:    time.Now(),
		}
		if err := db.Create(alert).Error; err != nil {
			t.Fatalf("failed to create alert: %v", err)
		}
		db.Preload("Domain").First(alert, alert.ID)

		// Create channels and rules with random severity assignments
		dispatcher := NewDispatcher(db, zapLogger)
		channels := make([]*trackingChannel, numChannels)
		expectedChannelIDs := make(map[uint]bool) // channels that SHOULD receive the notification

		for i := 0; i < numChannels; i++ {
			channelID := uint(i + 1)
			channels[i] = newTrackingChannel(channelID)
			dispatcher.RegisterChannel(channelID, channels[i])

			// Randomly assign a severity to this channel's rule
			ruleSeverityIdx := rapid.IntRange(0, len(severities)-1).Draw(t, "ruleSeverityIdx")
			ruleSeverity := severities[ruleSeverityIdx]

			rule := &domain.NotificationRule{
				DomainID:       dom.ID,
				ChannelID:      channelID,
				SeverityFilter: ruleSeverity,
			}
			if err := db.Create(rule).Error; err != nil {
				t.Fatalf("failed to create rule: %v", err)
			}

			// If this rule matches the alert severity, expect this channel to receive notification
			if ruleSeverity == alertSeverity {
				expectedChannelIDs[channelID] = true
			}
		}

		// Dispatch the alert
		if err := dispatcher.DispatchAlert(context.Background(), alert); err != nil {
			t.Fatalf("DispatchAlert failed: %v", err)
		}

		// Verify exactly the expected channels received notifications
		for i, ch := range channels {
			channelID := uint(i + 1)
			if expectedChannelIDs[channelID] {
				if len(ch.sent) != 1 {
					t.Fatalf("channel %d (matching severity %q) should have received 1 notification, got %d",
						channelID, alertSeverity, len(ch.sent))
				}
				// Verify the notification content matches the alert
				if ch.sent[0].Severity != alertSeverity {
					t.Fatalf("channel %d received notification with severity %q, want %q",
						channelID, ch.sent[0].Severity, alertSeverity)
				}
			} else {
				if len(ch.sent) != 0 {
					t.Fatalf("channel %d (non-matching severity) should have received 0 notifications, got %d",
						channelID, len(ch.sent))
				}
			}
		}
	})
}

// TestProperty22_WebhookPayloadCompleteness verifies that for any Notification input
// with non-empty fields, the WebhookPayload JSON contains all 6 required fields
// (alert_severity, alert_type, triggered_at, domain_name, domain_url, message)
// and none are empty.
//
// **Validates: Requirements 5.6**
func TestProperty22_WebhookPayloadCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random non-empty notification fields
		severity := rapid.SampledFrom([]string{"informational", "warning", "critical", "expired"}).Draw(t, "severity")
		alertType := rapid.SampledFrom([]string{"expiration", "certificate", "downtime", "email", "dns"}).Draw(t, "alertType")
		domainName := rapid.StringMatching(`[a-z][a-z0-9]{2,20}\.[a-z]{2,6}`).Draw(t, "domainName")
		domainURL := "/domains/" + rapid.StringMatching(`[0-9]{1,5}`).Draw(t, "domainID")
		message := rapid.StringMatching(`[A-Za-z0-9 ]{5,100}`).Draw(t, "message")

		// Generate a random time (within reasonable range)
		year := rapid.IntRange(2020, 2030).Draw(t, "year")
		month := rapid.IntRange(1, 12).Draw(t, "month")
		day := rapid.IntRange(1, 28).Draw(t, "day")
		triggeredAt := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)

		// Build WebhookPayload (same as Send() logic in webhook.go)
		payload := WebhookPayload{
			AlertSeverity: severity,
			AlertType:     alertType,
			TriggeredAt:   triggeredAt,
			DomainName:    domainName,
			DomainURL:     domainURL,
			Message:       message,
		}

		// Marshal to JSON
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to marshal WebhookPayload: %v", err)
		}

		// Unmarshal into generic map to check field presence
		var result map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &result); err != nil {
			t.Fatalf("failed to unmarshal payload JSON: %v", err)
		}

		// Property: All 6 required fields must exist in the JSON
		requiredFields := []string{"alert_severity", "alert_type", "triggered_at", "domain_name", "domain_url", "message"}
		for _, field := range requiredFields {
			val, exists := result[field]
			if !exists {
				t.Fatalf("required field %q missing from webhook payload JSON", field)
			}
			// Property: non-empty string values when notification fields are non-empty
			strVal, ok := val.(string)
			if ok && strVal == "" {
				t.Fatalf("required field %q is empty in webhook payload JSON", field)
			}
		}

		// Property: Verify that serialized values match inputs
		if result["alert_severity"] != severity {
			t.Fatalf("alert_severity mismatch: got %v, want %v", result["alert_severity"], severity)
		}
		if result["alert_type"] != alertType {
			t.Fatalf("alert_type mismatch: got %v, want %v", result["alert_type"], alertType)
		}
		if result["domain_name"] != domainName {
			t.Fatalf("domain_name mismatch: got %v, want %v", result["domain_name"], domainName)
		}
		if result["domain_url"] != domainURL {
			t.Fatalf("domain_url mismatch: got %v, want %v", result["domain_url"], domainURL)
		}
		if result["message"] != message {
			t.Fatalf("message mismatch: got %v, want %v", result["message"], message)
		}
		// triggered_at is serialized as RFC3339; verify it's not empty
		triggeredAtStr, ok := result["triggered_at"].(string)
		if !ok || triggeredAtStr == "" {
			t.Fatalf("triggered_at should be a non-empty string, got %v", result["triggered_at"])
		}
		// Parse triggered_at back to verify round-trip
		parsedTime, err := time.Parse(time.RFC3339, triggeredAtStr)
		if err != nil {
			t.Fatalf("triggered_at is not valid RFC3339: %v", err)
		}
		if !parsedTime.Equal(triggeredAt) {
			t.Fatalf("triggered_at round-trip failed: got %v, want %v", parsedTime, triggeredAt)
		}
	})
}
