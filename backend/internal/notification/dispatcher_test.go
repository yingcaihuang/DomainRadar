package notification

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// mockChannel implements NotificationChannel for testing.
type mockChannel struct {
	mu       sync.Mutex
	sendErr  error
	sent     []*Notification
	sendFunc func(ctx context.Context, n *Notification) error
}

func newMockChannel() *mockChannel {
	return &mockChannel{
		sent: make([]*Notification, 0),
	}
}

func (m *mockChannel) Send(ctx context.Context, notification *Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendFunc != nil {
		return m.sendFunc(ctx, notification)
	}
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, notification)
	return nil
}

func (m *mockChannel) TestConnection(ctx context.Context, config *ChannelConfig) error {
	return nil
}

func (m *mockChannel) ChannelType() string {
	return "mock"
}

func (m *mockChannel) getSent() []*Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*Notification(nil), m.sent...)
}

// setupTestDB creates an in-memory SQLite database with required tables.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// Migrate required tables
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

// createTestAlert creates and persists a test alert.
func createTestAlert(t *testing.T, db *gorm.DB, severity string) *domain.Alert {
	t.Helper()
	d := &domain.NormalizedDomain{
		DomainName:     "example.com",
		DataSourceType: "manual",
		Status:         "active",
	}
	require.NoError(t, db.Create(d).Error)

	alert := &domain.Alert{
		DomainID:       d.ID,
		AlertType:      "expiration",
		Severity:       severity,
		Message:        "Domain expires in 7 days",
		DeliveryStatus: "pending",
		GeneratedAt:    time.Now(),
	}
	require.NoError(t, db.Create(alert).Error)

	// Reload with domain association
	db.Preload("Domain").First(alert, alert.ID)
	return alert
}

// createTestRule creates a notification rule mapping severity to a channel.
func createTestRule(t *testing.T, db *gorm.DB, channelID uint, severity string) {
	t.Helper()
	// Create a domain for the rule (uses domain_id = 0 for global rules in this test)
	rule := &domain.NotificationRule{
		DomainID:       1,
		ChannelID:      channelID,
		SeverityFilter: severity,
	}
	require.NoError(t, db.Create(rule).Error)
}

// --- CalculateNotificationBackoff tests ---

func TestCalculateNotificationBackoff_Retry0(t *testing.T) {
	backoff := CalculateNotificationBackoff(0)
	assert.Equal(t, 5*time.Second, backoff)
}

func TestCalculateNotificationBackoff_Retry1(t *testing.T) {
	backoff := CalculateNotificationBackoff(1)
	assert.Equal(t, 10*time.Second, backoff)
}

func TestCalculateNotificationBackoff_Retry2(t *testing.T) {
	backoff := CalculateNotificationBackoff(2)
	assert.Equal(t, 20*time.Second, backoff)
}

func TestCalculateNotificationBackoff_ExponentialPattern(t *testing.T) {
	for i := 0; i < MaxRetries; i++ {
		expected := BaseBackoff * time.Duration(1<<i)
		actual := CalculateNotificationBackoff(i)
		assert.Equal(t, expected, actual, "retry %d backoff mismatch", i)
	}
}

// --- NewDispatcher tests ---

func TestNewDispatcher(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	assert.NotNil(t, dispatcher)
	assert.NotNil(t, dispatcher.channels)
	assert.Equal(t, 0, len(dispatcher.channels))
}

// --- RegisterChannel tests ---

func TestRegisterChannel(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()

	dispatcher.RegisterChannel(1, ch)
	assert.Equal(t, 1, len(dispatcher.channels))

	dispatcher.RegisterChannel(2, ch)
	assert.Equal(t, 2, len(dispatcher.channels))
}

func TestRegisterChannel_Overwrite(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch1 := newMockChannel()
	ch2 := newMockChannel()

	dispatcher.RegisterChannel(1, ch1)
	dispatcher.RegisterChannel(1, ch2)
	assert.Equal(t, 1, len(dispatcher.channels))
}

// --- DispatchAlert tests ---

func TestDispatchAlert_Success(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "critical")
	createTestRule(t, db, 1, "critical")

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err)

	// Verify notification was sent
	sent := ch.getSent()
	assert.Equal(t, 1, len(sent))
	assert.Equal(t, alert.ID, sent[0].AlertID)
	assert.Equal(t, "critical", sent[0].Severity)
	assert.Equal(t, "example.com", sent[0].DomainName)

	// Verify notification log was created
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	assert.Equal(t, 1, len(logs))
	assert.Equal(t, StatusSent, logs[0].Status)
	assert.NotNil(t, logs[0].SentAt)

	// Verify alert delivery status updated
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, DeliveryStatusDelivered, updatedAlert.DeliveryStatus)
}

func TestDispatchAlert_NoMatchingRules(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "informational")
	// No rules created for "informational" severity

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err)

	// Verify no notification was sent
	sent := ch.getSent()
	assert.Equal(t, 0, len(sent))
}

func TestDispatchAlert_ChannelFailure_RecordsFailure(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	ch.sendErr = errors.New("connection refused")
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "warning")
	createTestRule(t, db, 1, "warning")

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err) // DispatchAlert itself doesn't error on channel failure

	// Verify failure was recorded
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	assert.Equal(t, 1, len(logs))
	assert.Equal(t, StatusFailed, logs[0].Status)
	assert.Contains(t, logs[0].ErrorReason, "connection refused")
	assert.Equal(t, 0, logs[0].RetryCount)
}

func TestDispatchAlert_AllChannelsFail_Critical_FlagUndelivered(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	ch.sendErr = errors.New("timeout")
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "critical")
	createTestRule(t, db, 1, "critical")

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err)

	// Verify alert is flagged as undelivered
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, DeliveryStatusUndelivered, updatedAlert.DeliveryStatus)
}

func TestDispatchAlert_PartialSuccess(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)

	successCh := newMockChannel()
	failCh := newMockChannel()
	failCh.sendErr = errors.New("send error")

	dispatcher.RegisterChannel(1, successCh)
	dispatcher.RegisterChannel(2, failCh)

	alert := createTestAlert(t, db, "critical")

	// Create rules for both channels
	rule1 := &domain.NotificationRule{DomainID: 1, ChannelID: 1, SeverityFilter: "critical"}
	rule2 := &domain.NotificationRule{DomainID: 1, ChannelID: 2, SeverityFilter: "critical"}
	require.NoError(t, db.Create(rule1).Error)
	require.NoError(t, db.Create(rule2).Error)

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err)

	// Alert should be marked as delivered since at least one channel succeeded
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, DeliveryStatusDelivered, updatedAlert.DeliveryStatus)

	// Verify one success and one failure log
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	assert.Equal(t, 2, len(logs))

	var sentCount, failedCount int
	for _, l := range logs {
		if l.Status == StatusSent {
			sentCount++
		} else if l.Status == StatusFailed {
			failedCount++
		}
	}
	assert.Equal(t, 1, sentCount)
	assert.Equal(t, 1, failedCount)
}

func TestDispatchAlert_ChannelNotRegistered(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	// Don't register channel ID 1

	alert := createTestAlert(t, db, "warning")
	createTestRule(t, db, 1, "warning")

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err)

	// Should record failure for unregistered channel
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	assert.Equal(t, 1, len(logs))
	assert.Equal(t, StatusFailed, logs[0].Status)
	assert.Contains(t, logs[0].ErrorReason, "channel not registered")
}

func TestDispatchAlert_ChannelError_RecordsErrorReason(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)

	// Channel that returns a specific error
	errCh := newMockChannel()
	errCh.sendFunc = func(ctx context.Context, n *Notification) error {
		return errors.New("SMTP connection timed out after 30s")
	}
	dispatcher.RegisterChannel(1, errCh)

	alert := createTestAlert(t, db, "warning")
	createTestRule(t, db, 1, "warning")

	err := dispatcher.DispatchAlert(context.Background(), alert)
	require.NoError(t, err)

	// Verify failure was recorded with the error reason
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	require.Equal(t, 1, len(logs))
	assert.Equal(t, StatusFailed, logs[0].Status)
	assert.Contains(t, logs[0].ErrorReason, "SMTP connection timed out after 30s")
	assert.NotNil(t, logs[0].SentAt)
}

// --- RetryFailedNotifications tests ---

func TestRetryFailedNotifications_Success(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "warning")

	// Create a failed log that's old enough to retry
	pastTime := time.Now().Add(-1 * time.Minute)
	failedLog := &domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   1,
		Status:      StatusFailed,
		ErrorReason: "temporary error",
		RetryCount:  0,
		SentAt:      &pastTime,
		CreatedAt:   pastTime,
	}
	require.NoError(t, db.Create(failedLog).Error)

	err := dispatcher.RetryFailedNotifications(context.Background())
	require.NoError(t, err)

	// Verify log updated to sent
	var log domain.NotificationLog
	db.First(&log, failedLog.ID)
	assert.Equal(t, StatusSent, log.Status)
	assert.Equal(t, 1, log.RetryCount)
}

func TestRetryFailedNotifications_ExceedsMaxRetries(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	ch.sendErr = errors.New("persistent failure")
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "critical")

	// Create a failed log at retry_count = 2 (one more retry allowed)
	pastTime := time.Now().Add(-1 * time.Minute)
	failedLog := &domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   1,
		Status:      StatusFailed,
		ErrorReason: "persistent failure",
		RetryCount:  2,
		SentAt:      &pastTime,
		CreatedAt:   pastTime,
	}
	require.NoError(t, db.Create(failedLog).Error)

	err := dispatcher.RetryFailedNotifications(context.Background())
	require.NoError(t, err)

	// Verify retry count incremented and still failed
	var log domain.NotificationLog
	db.First(&log, failedLog.ID)
	assert.Equal(t, StatusFailed, log.Status)
	assert.Equal(t, 3, log.RetryCount)

	// Verify critical alert flagged as undelivered
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, DeliveryStatusUndelivered, updatedAlert.DeliveryStatus)
}

func TestRetryFailedNotifications_SkipsRecentFailures(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "warning")

	// Create a failed log that's too recent (backoff not elapsed)
	now := time.Now()
	failedLog := &domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   1,
		Status:      StatusFailed,
		ErrorReason: "temporary error",
		RetryCount:  0,
		SentAt:      &now, // Just failed now, need to wait 5s
		CreatedAt:   now,
	}
	require.NoError(t, db.Create(failedLog).Error)

	err := dispatcher.RetryFailedNotifications(context.Background())
	require.NoError(t, err)

	// Verify log was NOT retried (still at retry_count 0)
	var log domain.NotificationLog
	db.First(&log, failedLog.ID)
	assert.Equal(t, StatusFailed, log.Status)
	assert.Equal(t, 0, log.RetryCount)

	// Verify no messages were sent
	sent := ch.getSent()
	assert.Equal(t, 0, len(sent))
}

func TestRetryFailedNotifications_MaxRetryReached_NotRetried(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "warning")

	// Create a log that's already at max retries
	pastTime := time.Now().Add(-10 * time.Minute)
	failedLog := &domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   1,
		Status:      StatusFailed,
		ErrorReason: "permanent failure",
		RetryCount:  3, // Already at max
		SentAt:      &pastTime,
		CreatedAt:   pastTime,
	}
	require.NoError(t, db.Create(failedLog).Error)

	err := dispatcher.RetryFailedNotifications(context.Background())
	require.NoError(t, err)

	// Query filters retry_count < MaxRetries, so this should not be picked up
	sent := ch.getSent()
	assert.Equal(t, 0, len(sent))
}

func TestRetryFailedNotifications_CriticalAlert_5MinDelay(t *testing.T) {
	db := setupTestDB(t)
	zapLogger, _ := createTestLogger()

	dispatcher := NewDispatcher(db, zapLogger)
	ch := newMockChannel()
	dispatcher.RegisterChannel(1, ch)

	alert := createTestAlert(t, db, "critical")
	// Mark alert as undelivered
	db.Model(&domain.Alert{}).Where("id = ?", alert.ID).
		Update("delivery_status", DeliveryStatusUndelivered)

	// Create a failed log that's 2 minutes old (less than 5-minute requirement)
	twoMinAgo := time.Now().Add(-2 * time.Minute)
	failedLog := &domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   1,
		Status:      StatusFailed,
		ErrorReason: "temporary",
		RetryCount:  1,
		SentAt:      &twoMinAgo,
		CreatedAt:   twoMinAgo,
	}
	require.NoError(t, db.Create(failedLog).Error)

	err := dispatcher.RetryFailedNotifications(context.Background())
	require.NoError(t, err)

	// Should not be retried due to 5-minute delay requirement
	sent := ch.getSent()
	assert.Equal(t, 0, len(sent))
}

// --- Helper functions ---

func createTestLogger() (*zap.Logger, error) {
	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zap.ErrorLevel) // Quiet during tests
	return config.Build()
}
