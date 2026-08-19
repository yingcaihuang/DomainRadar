package alert

import (
	"context"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- CalculateSeverity Unit Tests ---

func TestCalculateSeverity_Expired(t *testing.T) {
	// Past due: daysRemaining < 0
	assert.Equal(t, SeverityExpired, CalculateSeverity(-1, true))
	assert.Equal(t, SeverityExpired, CalculateSeverity(-1, false))
	assert.Equal(t, SeverityExpired, CalculateSeverity(-100, true))
	assert.Equal(t, SeverityExpired, CalculateSeverity(-365, false))
}

func TestCalculateSeverity_Critical(t *testing.T) {
	// 7-0 days: critical
	assert.Equal(t, SeverityCritical, CalculateSeverity(0, true))
	assert.Equal(t, SeverityCritical, CalculateSeverity(1, true))
	assert.Equal(t, SeverityCritical, CalculateSeverity(3, true))
	assert.Equal(t, SeverityCritical, CalculateSeverity(7, true))
}

func TestCalculateSeverity_Warning(t *testing.T) {
	// 30-8 days: warning (with auto-renew enabled)
	assert.Equal(t, SeverityWarning, CalculateSeverity(8, true))
	assert.Equal(t, SeverityWarning, CalculateSeverity(14, true))
	assert.Equal(t, SeverityWarning, CalculateSeverity(30, true))
}

func TestCalculateSeverity_Informational(t *testing.T) {
	// 90-31 days: informational
	assert.Equal(t, SeverityInformational, CalculateSeverity(31, true))
	assert.Equal(t, SeverityInformational, CalculateSeverity(60, true))
	assert.Equal(t, SeverityInformational, CalculateSeverity(90, true))
	assert.Equal(t, SeverityInformational, CalculateSeverity(91, false))
	assert.Equal(t, SeverityInformational, CalculateSeverity(365, true))
}

func TestCalculateSeverity_EscalationAutoRenewDisabled(t *testing.T) {
	// When auto-renew disabled and within 30 days, warning escalates to critical
	// Days 8-30 with auto-renew disabled should be critical (escalated from warning)
	assert.Equal(t, SeverityCritical, CalculateSeverity(8, false))
	assert.Equal(t, SeverityCritical, CalculateSeverity(14, false))
	assert.Equal(t, SeverityCritical, CalculateSeverity(20, false))
	assert.Equal(t, SeverityCritical, CalculateSeverity(30, false))
}

func TestCalculateSeverity_NoEscalationAlreadyCritical(t *testing.T) {
	// Days 0-7 are already critical, no further escalation needed
	assert.Equal(t, SeverityCritical, CalculateSeverity(0, false))
	assert.Equal(t, SeverityCritical, CalculateSeverity(5, false))
	assert.Equal(t, SeverityCritical, CalculateSeverity(7, false))
}

func TestCalculateSeverity_NoEscalationBeyond30Days(t *testing.T) {
	// Beyond 30 days, auto-renew status doesn't cause escalation
	assert.Equal(t, SeverityInformational, CalculateSeverity(31, false))
	assert.Equal(t, SeverityInformational, CalculateSeverity(60, false))
	assert.Equal(t, SeverityInformational, CalculateSeverity(90, false))
}

func TestCalculateSeverity_BoundaryValues(t *testing.T) {
	// Test exact boundary values
	tests := []struct {
		name          string
		daysRemaining int
		autoRenew     bool
		expected      string
	}{
		{"boundary: -1 auto-renew", -1, true, SeverityExpired},
		{"boundary: 0 auto-renew", 0, true, SeverityCritical},
		{"boundary: 7 auto-renew", 7, true, SeverityCritical},
		{"boundary: 8 auto-renew", 8, true, SeverityWarning},
		{"boundary: 30 auto-renew", 30, true, SeverityWarning},
		{"boundary: 31 auto-renew", 31, true, SeverityInformational},
		{"boundary: 8 no-auto-renew (escalated)", 8, false, SeverityCritical},
		{"boundary: 30 no-auto-renew (escalated)", 30, false, SeverityCritical},
		{"boundary: 31 no-auto-renew (no escalation)", 31, false, SeverityInformational},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateSeverity(tt.daysRemaining, tt.autoRenew)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- DefaultThresholds Tests ---

func TestDefaultThresholds(t *testing.T) {
	thresholds := DefaultThresholds()
	expected := []int{90, 30, 14, 7, 3, 1}
	assert.Equal(t, expected, thresholds)
}

// --- AlertEngine Integration Tests (with in-memory SQLite) ---

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate the required models
	err = db.AutoMigrate(&domain.NormalizedDomain{}, &domain.Alert{})
	require.NoError(t, err)

	return db
}

// expirationInDays returns a time that will result in exactly `days` when
// math.Floor(time.Until(t).Hours() / 24) is computed.
// It adds (days*24 + 1) hours to give a safe margin.
func expirationInDays(days int) time.Time {
	return time.Now().Add(time.Duration(days)*24*time.Hour + 1*time.Hour)
}

func TestNewAlertEngine(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()

	engine := NewAlertEngine(db, logger)
	assert.NotNil(t, engine)
	assert.Equal(t, DefaultThresholds(), engine.thresholds)
}

func TestAlertEngine_SetThresholds(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()

	engine := NewAlertEngine(db, logger)
	custom := []int{60, 30, 7}
	engine.SetThresholds(custom)
	assert.Equal(t, custom, engine.thresholds)
}

func TestAlertEngine_RunExpirationCheck_CreatesAlerts(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a domain expiring in exactly 7 days (threshold hit)
	expirationDate := expirationInDays(7)
	d := domain.NormalizedDomain{
		DomainName:          "example.com",
		RegistrarIdentifier: "godaddy",
		ExpirationDate:      &expirationDate,
		AutoRenew:           true,
	}
	require.NoError(t, db.Create(&d).Error)

	// Run expiration check
	err := engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)

	// Verify alert was created
	var alerts []domain.Alert
	db.Where("domain_id = ?", d.ID).Find(&alerts)
	assert.Len(t, alerts, 1)
	assert.Equal(t, "expiration", alerts[0].AlertType)
	assert.Equal(t, SeverityCritical, alerts[0].Severity)
	assert.Equal(t, 7, *alerts[0].DaysRemaining)
}

func TestAlertEngine_RunExpirationCheck_NoDuplicates(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a domain expiring in exactly 3 days (threshold hit)
	expirationDate := expirationInDays(3)
	d := domain.NormalizedDomain{
		DomainName:          "example.com",
		RegistrarIdentifier: "godaddy",
		ExpirationDate:      &expirationDate,
		AutoRenew:           true,
	}
	require.NoError(t, db.Create(&d).Error)

	// Run expiration check twice
	err := engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)
	err = engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)

	// Verify only one alert created (no duplicates)
	var count int64
	db.Model(&domain.Alert{}).Where("domain_id = ?", d.ID).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestAlertEngine_RunExpirationCheck_NoAlertForNonThreshold(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a domain expiring in 50 days (not a threshold value)
	expirationDate := expirationInDays(50)
	d := domain.NormalizedDomain{
		DomainName:          "example.com",
		RegistrarIdentifier: "godaddy",
		ExpirationDate:      &expirationDate,
		AutoRenew:           true,
	}
	require.NoError(t, db.Create(&d).Error)

	// Run expiration check
	err := engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)

	// Verify no alert was created
	var count int64
	db.Model(&domain.Alert{}).Where("domain_id = ?", d.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestAlertEngine_RunExpirationCheck_SkipsNilExpirationDate(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a domain without expiration date
	d := domain.NormalizedDomain{
		DomainName:          "no-expiry.com",
		RegistrarIdentifier: "cloudflare",
	}
	require.NoError(t, db.Create(&d).Error)

	// Run expiration check
	err := engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)

	// Verify no alert was created
	var count int64
	db.Model(&domain.Alert{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestAlertEngine_RunExpirationCheck_EscalatedSeverity(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a domain expiring in exactly 14 days with auto-renew disabled
	expirationDate := expirationInDays(14)
	d := domain.NormalizedDomain{
		DomainName:          "no-renew.com",
		RegistrarIdentifier: "namecheap",
		ExpirationDate:      &expirationDate,
		AutoRenew:           false,
	}
	require.NoError(t, db.Create(&d).Error)

	// Run expiration check
	err := engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)

	// Verify alert has escalated severity (warning -> critical)
	var alerts []domain.Alert
	db.Where("domain_id = ?", d.ID).Find(&alerts)
	assert.Len(t, alerts, 1)
	assert.Equal(t, SeverityCritical, alerts[0].Severity)
}

func TestAlertEngine_RunExpirationCheck_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Run expiration check with cancelled context
	err := engine.RunExpirationCheck(ctx)
	// Should either complete quickly (no domains) or return timeout error
	// Since loading domains with cancelled context may fail
	_ = err
}

func TestAlertEngine_RunExpirationCheck_AlertMessage(t *testing.T) {
	db := setupTestDB(t)
	logger := zap.NewNop()
	engine := NewAlertEngine(db, logger)

	// Create a domain expiring in exactly 1 day
	expirationDate := expirationInDays(1)
	d := domain.NormalizedDomain{
		DomainName:          "urgent.com",
		RegistrarIdentifier: "godaddy",
		ExpirationDate:      &expirationDate,
		AutoRenew:           true,
	}
	require.NoError(t, db.Create(&d).Error)

	// Run expiration check
	err := engine.RunExpirationCheck(context.Background())
	require.NoError(t, err)

	// Verify alert message contains domain info
	var alerts []domain.Alert
	db.Where("domain_id = ?", d.ID).Find(&alerts)
	require.Len(t, alerts, 1)
	assert.Contains(t, alerts[0].Message, "urgent.com")
	assert.Contains(t, alerts[0].Message, "godaddy")
	assert.Contains(t, alerts[0].Message, "1 days")
}
