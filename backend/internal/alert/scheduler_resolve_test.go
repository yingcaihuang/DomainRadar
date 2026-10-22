package alert

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"domainradar/internal/domain"
)

func setupResolveTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.NormalizedDomain{}, &domain.Tag{}, &domain.Alert{}))
	return db
}

// Renewal must auto-close the stale "expired" alert; a later re-expiry must
// be able to raise a fresh alert (dedup unblocked).
func TestRunExpirationCheck_AutoResolvesClearedAlerts(t *testing.T) {
	db := setupResolveTestDB(t)

	// Domain currently expired, with an open critical alert.
	expired := time.Now().Add(-10 * 24 * time.Hour)
	d := domain.NormalizedDomain{DomainName: "renewed.com", ExpirationDate: &expired, Status: "expired"}
	require.NoError(t, db.Create(&d).Error)
	oldAlert := domain.Alert{
		DomainID: d.ID, AlertType: "domain_expired", Severity: "critical",
		Message: "域名 renewed.com 已过期 10 天", Acknowledged: false, GeneratedAt: time.Now(),
	}
	require.NoError(t, db.Create(&oldAlert).Error)

	// Domain renews far out; alert scheduler runs.
	renewed := time.Now().Add(200 * 24 * time.Hour)
	require.NoError(t, db.Model(&d).Update("expiration_date", renewed).Error)

	s := NewAlertScheduler(db, zap.NewNop())
	require.NoError(t, s.RunExpirationCheck(context.Background()))

	var got domain.Alert
	require.NoError(t, db.First(&got, oldAlert.ID).Error)
	assert.True(t, got.Acknowledged, "stale expired alert must be auto-resolved after renewal")
	require.NotNil(t, got.AcknowledgedAt)
}

// A short renewal (still within 30 days) closes the expired alert but keeps
// the 30d warning zone active.
func TestRunExpirationCheck_TieredResolution(t *testing.T) {
	db := setupResolveTestDB(t)

	expired := time.Now().Add(-5 * 24 * time.Hour)
	d := domain.NormalizedDomain{DomainName: "short-renewal.com", ExpirationDate: &expired, Status: "expired"}
	require.NoError(t, db.Create(&d).Error)
	expiredAlert := domain.Alert{DomainID: d.ID, AlertType: "domain_expired", Severity: "critical", Acknowledged: false, GeneratedAt: time.Now()}
	warnAlert := domain.Alert{DomainID: d.ID, AlertType: "domain_expiring_30d", Severity: "warning", Acknowledged: false, GeneratedAt: time.Now()}
	require.NoError(t, db.Create(&expiredAlert).Error)
	require.NoError(t, db.Create(&warnAlert).Error)

	// Renew to 15 days out: expired clears, 30d warning stays applicable.
	renewed := time.Now().Add(15 * 24 * time.Hour)
	require.NoError(t, db.Model(&d).Update("expiration_date", renewed).Error)

	s := NewAlertScheduler(db, zap.NewNop())
	require.NoError(t, s.RunExpirationCheck(context.Background()))

	var gotExpired domain.Alert
	require.NoError(t, db.First(&gotExpired, expiredAlert.ID).Error)
	assert.True(t, gotExpired.Acknowledged, "expired alert must clear once no longer expired")

	var gotWarn domain.Alert
	require.NoError(t, db.First(&gotWarn, warnAlert.ID).Error)
	assert.False(t, gotWarn.Acknowledged, "30d warning stays applicable within 30 days")
}

// After auto-resolution, a re-expiry must be able to create a fresh alert
// (the old dedup suppression is gone).
func TestRunExpirationCheck_ResolvedAlertDoesNotSuppressNewOnes(t *testing.T) {
	db := setupResolveTestDB(t)

	expired := time.Now().Add(-10 * 24 * time.Hour)
	d := domain.NormalizedDomain{DomainName: "cycle.com", ExpirationDate: &expired, Status: "expired"}
	require.NoError(t, db.Create(&d).Error)
	stale := domain.Alert{DomainID: d.ID, AlertType: "domain_expired", Severity: "critical", Acknowledged: false, GeneratedAt: time.Now()}
	require.NoError(t, db.Create(&stale).Error)

	s := NewAlertScheduler(db, zap.NewNop())

	// Round 1: renewal resolves the stale alert.
	renewed := time.Now().Add(200 * 24 * time.Hour)
	require.NoError(t, db.Model(&d).Update("expiration_date", renewed).Error)
	require.NoError(t, s.RunExpirationCheck(context.Background()))

	// Round 2: domain expires again — a new alert must be created.
	expiredAgain := time.Now().Add(-3 * 24 * time.Hour)
	require.NoError(t, db.Model(&d).Update("expiration_date", expiredAgain).Error)
	require.NoError(t, s.RunExpirationCheck(context.Background()))

	var alerts []domain.Alert
	require.NoError(t, db.Where("domain_id = ? AND alert_type = ?", d.ID, "domain_expired").Order("id").Find(&alerts).Error)
	require.Len(t, alerts, 2, "re-expiry must raise a fresh alert after auto-resolution")
	assert.True(t, alerts[0].Acknowledged, "the stale alert must be resolved")
	assert.False(t, alerts[1].Acknowledged, "the fresh alert must be open")
}
