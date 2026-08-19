package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"domainradar/internal/domain"
	"domainradar/internal/notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- Mock Channel ---

// mockChannel implements notification.NotificationChannel for testing.
type mockChannel struct {
	channelType string
	sendErr     error
	sendCount   int
}

func (m *mockChannel) Send(ctx context.Context, n *notification.Notification) error {
	m.sendCount++
	return m.sendErr
}

func (m *mockChannel) TestConnection(ctx context.Context, config *notification.ChannelConfig) error {
	return nil
}

func (m *mockChannel) ChannelType() string {
	return m.channelType
}

// --- Test Helpers ---

func setupDeliveryTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
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

func createTestDomain(t *testing.T, db *gorm.DB) domain.NormalizedDomain {
	dom := domain.NormalizedDomain{
		DomainName:          "test-delivery.com",
		RegistrarIdentifier: "godaddy",
		Status:              "active",
	}
	require.NoError(t, db.Create(&dom).Error)
	return dom
}

func createTestAlert(t *testing.T, db *gorm.DB, domainID uint) domain.Alert {
	alert := domain.Alert{
		DomainID:       domainID,
		AlertType:      "expiration",
		Severity:       "critical",
		Message:        "Domain test-delivery.com expires in 3 days",
		DeliveryStatus: "pending",
		GeneratedAt:    time.Now(),
	}
	require.NoError(t, db.Create(&alert).Error)
	return alert
}

func createTestChannel(t *testing.T, db *gorm.DB, channelType string) domain.NotificationChannel {
	ch := domain.NotificationChannel{
		ChannelType: channelType,
		Name:        "Test " + channelType,
		Status:      "active",
	}
	require.NoError(t, db.Create(&ch).Error)
	return ch
}

func createTestRule(t *testing.T, db *gorm.DB, domainID, channelID uint, severity string) domain.NotificationRule {
	rule := domain.NotificationRule{
		DomainID:       domainID,
		ChannelID:      channelID,
		SeverityFilter: severity,
	}
	require.NoError(t, db.Create(&rule).Error)
	return rule
}

// --- DeliverAlert Tests ---

func TestNewAlertDeliveryService(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()

	svc := NewAlertDeliveryService(db, logger)
	assert.NotNil(t, svc)
	assert.NotNil(t, svc.notificationChannels)
}

func TestDeliverAlert_Success(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Register a successful mock channel
	mock := &mockChannel{channelType: "email", sendErr: nil}
	svc.RegisterChannel("email", mock)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "email")
	createTestRule(t, db, dom.ID, ch.ID, "critical")

	// Deliver
	err := svc.DeliverAlert(context.Background(), &alert)
	require.NoError(t, err)

	// Verify channel was called
	assert.Equal(t, 1, mock.sendCount)

	// Verify notification log was created with status "sent"
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	require.Len(t, logs, 1)
	assert.Equal(t, "sent", logs[0].Status)
	assert.Equal(t, ch.ID, logs[0].ChannelID)
	assert.NotNil(t, logs[0].SentAt)

	// Verify alert delivery status updated
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "delivered", updatedAlert.DeliveryStatus)
}

func TestDeliverAlert_ChannelFailure(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Register a failing mock channel
	mock := &mockChannel{channelType: "webhook", sendErr: errors.New("connection refused")}
	svc.RegisterChannel("webhook", mock)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "webhook")
	createTestRule(t, db, dom.ID, ch.ID, "critical")

	// Deliver
	err := svc.DeliverAlert(context.Background(), &alert)
	require.NoError(t, err)

	// Verify notification log was created with status "failed"
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	require.Len(t, logs, 1)
	assert.Equal(t, "failed", logs[0].Status)
	assert.Equal(t, "connection refused", logs[0].ErrorReason)
	assert.Equal(t, 0, logs[0].RetryCount)

	// Verify alert delivery status is "failed"
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "failed", updatedAlert.DeliveryStatus)
}

func TestDeliverAlert_MultipleChannels_PartialSuccess(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Register one succeeding and one failing channel
	mockEmail := &mockChannel{channelType: "email", sendErr: nil}
	mockWebhook := &mockChannel{channelType: "webhook", sendErr: errors.New("timeout")}
	svc.RegisterChannel("email", mockEmail)
	svc.RegisterChannel("webhook", mockWebhook)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	chEmail := createTestChannel(t, db, "email")
	chWebhook := createTestChannel(t, db, "webhook")
	createTestRule(t, db, dom.ID, chEmail.ID, "critical")
	createTestRule(t, db, dom.ID, chWebhook.ID, "critical")

	// Deliver
	err := svc.DeliverAlert(context.Background(), &alert)
	require.NoError(t, err)

	// Verify both channels were called
	assert.Equal(t, 1, mockEmail.sendCount)
	assert.Equal(t, 1, mockWebhook.sendCount)

	// Verify two notification logs created
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	assert.Len(t, logs, 2)

	// Verify alert delivery status is "delivered" (at least one succeeded)
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "delivered", updatedAlert.DeliveryStatus)
}

func TestDeliverAlert_NoMatchingRules(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Set up test data with no matching rules
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	// No rules created for this domain/severity combo

	// Deliver
	err := svc.DeliverAlert(context.Background(), &alert)
	require.NoError(t, err)

	// Verify no notification logs created
	var count int64
	db.Model(&domain.NotificationLog{}).Where("alert_id = ?", alert.ID).Count(&count)
	assert.Equal(t, int64(0), count)

	// Alert should be marked as delivered (no channels = not a failure)
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "delivered", updatedAlert.DeliveryStatus)
}

func TestDeliverAlert_UnregisteredChannelType(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)
	// No channels registered

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "sms")
	createTestRule(t, db, dom.ID, ch.ID, "critical")

	// Deliver
	err := svc.DeliverAlert(context.Background(), &alert)
	require.NoError(t, err)

	// Verify notification log was created with "failed" due to missing implementation
	var logs []domain.NotificationLog
	db.Where("alert_id = ?", alert.ID).Find(&logs)
	require.Len(t, logs, 1)
	assert.Equal(t, "failed", logs[0].Status)
	assert.Contains(t, logs[0].ErrorReason, "no channel implementation registered")
}

// --- RetryFailedDeliveries Tests ---

func TestRetryFailedDeliveries_Success(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Register a now-working channel
	mock := &mockChannel{channelType: "email", sendErr: nil}
	svc.RegisterChannel("email", mock)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "email")

	// Create a failed notification log older than 5 minutes
	failedLog := domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   ch.ID,
		Status:      "failed",
		ErrorReason: "connection refused",
		RetryCount:  0,
		CreatedAt:   time.Now().Add(-10 * time.Minute), // 10 minutes ago
	}
	require.NoError(t, db.Create(&failedLog).Error)

	// Run retry
	err := svc.RetryFailedDeliveries(context.Background())
	require.NoError(t, err)

	// Verify channel was called
	assert.Equal(t, 1, mock.sendCount)

	// Verify log updated to "sent"
	var updatedLog domain.NotificationLog
	db.First(&updatedLog, failedLog.ID)
	assert.Equal(t, "sent", updatedLog.Status)
	assert.Equal(t, 1, updatedLog.RetryCount)
	assert.NotNil(t, updatedLog.SentAt)

	// Verify alert delivery status updated
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "delivered", updatedAlert.DeliveryStatus)
}

func TestRetryFailedDeliveries_RetriesExhausted(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Register a still-failing channel
	mock := &mockChannel{channelType: "email", sendErr: errors.New("still failing")}
	svc.RegisterChannel("email", mock)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "email")

	// Create a failed log with 2 retries already done (one more will exhaust)
	failedLog := domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   ch.ID,
		Status:      "failed",
		ErrorReason: "previous error",
		RetryCount:  2,
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, db.Create(&failedLog).Error)

	// Run retry
	err := svc.RetryFailedDeliveries(context.Background())
	require.NoError(t, err)

	// Verify channel was called
	assert.Equal(t, 1, mock.sendCount)

	// Verify log has retry_count = 3 (maxed out)
	var updatedLog domain.NotificationLog
	db.First(&updatedLog, failedLog.ID)
	assert.Equal(t, "failed", updatedLog.Status)
	assert.Equal(t, 3, updatedLog.RetryCount)
	assert.Contains(t, updatedLog.ErrorReason, "still failing")

	// Verify alert delivery status is "failed" since all deliveries failed
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "failed", updatedAlert.DeliveryStatus)
}

func TestRetryFailedDeliveries_RespectsInterval(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	mock := &mockChannel{channelType: "email", sendErr: nil}
	svc.RegisterChannel("email", mock)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "email")

	// Create a failed log that is TOO RECENT (less than 5 minutes ago)
	recentLog := domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   ch.ID,
		Status:      "failed",
		ErrorReason: "timeout",
		RetryCount:  1,
		CreatedAt:   time.Now().Add(-2 * time.Minute), // Only 2 minutes ago
	}
	require.NoError(t, db.Create(&recentLog).Error)

	// Run retry
	err := svc.RetryFailedDeliveries(context.Background())
	require.NoError(t, err)

	// Verify channel was NOT called (interval not met)
	assert.Equal(t, 0, mock.sendCount)

	// Log should remain unchanged
	var updatedLog domain.NotificationLog
	db.First(&updatedLog, recentLog.ID)
	assert.Equal(t, "failed", updatedLog.Status)
	assert.Equal(t, 1, updatedLog.RetryCount)
}

func TestRetryFailedDeliveries_SkipsMaxedOutRetries(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	mock := &mockChannel{channelType: "email", sendErr: nil}
	svc.RegisterChannel("email", mock)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "email")

	// Create a log with retry_count already at max (3)
	maxedLog := domain.NotificationLog{
		AlertID:     alert.ID,
		ChannelID:   ch.ID,
		Status:      "failed",
		ErrorReason: "permanently failed",
		RetryCount:  3,
		CreatedAt:   time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, db.Create(&maxedLog).Error)

	// Run retry
	err := svc.RetryFailedDeliveries(context.Background())
	require.NoError(t, err)

	// Verify channel was NOT called (retry count >= max)
	assert.Equal(t, 0, mock.sendCount)
}

func TestRetryFailedDeliveries_ContextCancellation(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.RetryFailedDeliveries(ctx)
	// Should handle cancelled context gracefully
	_ = err
}

// --- CleanupOldLogs Tests ---

func TestCleanupOldLogs(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	// Set up test data
	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch := createTestChannel(t, db, "email")

	// Create a log older than 365 days
	oldLog := domain.NotificationLog{
		AlertID:   alert.ID,
		ChannelID: ch.ID,
		Status:    "sent",
		CreatedAt: time.Now().AddDate(0, 0, -400), // 400 days ago
	}
	require.NoError(t, db.Create(&oldLog).Error)

	// Create a recent log
	recentLog := domain.NotificationLog{
		AlertID:   alert.ID,
		ChannelID: ch.ID,
		Status:    "sent",
		CreatedAt: time.Now().Add(-24 * time.Hour), // yesterday
	}
	require.NoError(t, db.Create(&recentLog).Error)

	// Run cleanup
	err := svc.CleanupOldLogs(context.Background())
	require.NoError(t, err)

	// Verify old log deleted, recent log retained
	var count int64
	db.Model(&domain.NotificationLog{}).Count(&count)
	assert.Equal(t, int64(1), count)

	var remaining domain.NotificationLog
	db.First(&remaining)
	assert.Equal(t, recentLog.ID, remaining.ID)
}

// --- RegisterChannel Test ---

func TestRegisterChannel(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	mock := &mockChannel{channelType: "test_type"}
	svc.RegisterChannel("test_type", mock)

	assert.Equal(t, mock, svc.notificationChannels["test_type"])
}

// --- updateAlertDeliveryStatusIfAllFailed Tests ---

func TestUpdateAlertDeliveryStatus_AllFailed(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch1 := createTestChannel(t, db, "email")
	ch2 := createTestChannel(t, db, "webhook")

	// Create two failed logs with max retries
	for _, chID := range []uint{ch1.ID, ch2.ID} {
		log := domain.NotificationLog{
			AlertID:    alert.ID,
			ChannelID:  chID,
			Status:     "failed",
			RetryCount: MaxRetries,
			CreatedAt:  time.Now(),
		}
		require.NoError(t, db.Create(&log).Error)
	}

	// Call the method
	svc.updateAlertDeliveryStatusIfAllFailed(context.Background(), alert.ID)

	// Verify alert is marked as failed
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "failed", updatedAlert.DeliveryStatus)
}

func TestUpdateAlertDeliveryStatus_NotAllFailed(t *testing.T) {
	db := setupDeliveryTestDB(t)
	logger := zap.NewNop()
	svc := NewAlertDeliveryService(db, logger)

	dom := createTestDomain(t, db)
	alert := createTestAlert(t, db, dom.ID)
	ch1 := createTestChannel(t, db, "email")
	ch2 := createTestChannel(t, db, "webhook")

	// One failed, one sent
	failedLog := domain.NotificationLog{
		AlertID:    alert.ID,
		ChannelID:  ch1.ID,
		Status:     "failed",
		RetryCount: MaxRetries,
		CreatedAt:  time.Now(),
	}
	require.NoError(t, db.Create(&failedLog).Error)

	sentLog := domain.NotificationLog{
		AlertID:   alert.ID,
		ChannelID: ch2.ID,
		Status:    "sent",
		CreatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&sentLog).Error)

	// Call the method
	svc.updateAlertDeliveryStatusIfAllFailed(context.Background(), alert.ID)

	// Verify alert is NOT marked as failed (still "pending" from creation)
	var updatedAlert domain.Alert
	db.First(&updatedAlert, alert.ID)
	assert.Equal(t, "pending", updatedAlert.DeliveryStatus)
}
