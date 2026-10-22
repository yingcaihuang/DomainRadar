package sync

import (
	"context"
	"testing"
	"time"

	"domainradar/internal/adapter"
	"domainradar/internal/crypto"
	"domainradar/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type cycleAdapter struct {
	domains     []domain.NormalizedDomain
	credentials chan adapter.RegistrarCredential
}

func (a *cycleAdapter) RegistrarType() string { return "cloudflare" }
func (a *cycleAdapter) TestConnection(context.Context, *adapter.RegistrarCredential) error {
	return nil
}
func (a *cycleAdapter) GetDomainDetail(context.Context, *adapter.RegistrarCredential, string) (*domain.NormalizedDomain, error) {
	return nil, nil
}
func (a *cycleAdapter) ListDomains(_ context.Context, cred *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	a.credentials <- *cred
	return a.domains, nil
}

func TestRunSyncCycle_ImportOptionsAndCredentials(t *testing.T) {
	for _, fullImport := range []bool{true, false} {
		name := "existing-only"
		if fullImport {
			name = "full-import"
		}
		t.Run(name, func(t *testing.T) {
			db := setupTestDB(t)
			cryptoSvc, err := crypto.NewCryptoService("01234567890123456789012345678901")
			require.NoError(t, err)
			encrypted, err := cryptoSvc.Encrypt(`{"token":"test-token","account_id":"cf-account"}`)
			require.NoError(t, err)
			config := domain.RegistrarConfig{RegistrarType: "cloudflare", DisplayName: "Cloudflare"}
			require.NoError(t, db.Create(&config).Error)
			account := domain.RegistrarAccount{
				RegistrarConfigID: config.ID, AccountName: name, CredentialsEncrypted: encrypted, Status: "connected",
			}
			require.NoError(t, db.Create(&account).Error)
			require.NoError(t, db.Model(&account).Updates(map[string]interface{}{
				"full_import_enabled": fullImport, "auto_sync_enabled": false,
			}).Error)
			require.NoError(t, db.Create(&domain.NormalizedDomain{
				DomainName: "owned.test", RegistrarAccountID: &account.ID, Status: "active",
			}).Error)
			stub := &cycleAdapter{
				domains: []domain.NormalizedDomain{
					{DomainName: "owned.test", Status: "locked"},
					{DomainName: "new.test", Status: "active"},
				},
				credentials: make(chan adapter.RegistrarCredential, 1),
			}
			registry := adapter.NewAdapterRegistry()
			registry.Register(stub)
			scheduler := NewSyncScheduler(db, registry, cryptoSvc, zap.NewNop())
			// Disabling automatic sync must not block explicitly requested cycles.
			require.NoError(t, scheduler.RunSyncCycle(context.Background(), account.ID))
			cred := <-stub.credentials
			assert.Equal(t, "test-token", cred.Token)
			assert.Equal(t, "cf-account", cred.Extra["account_id"])
			require.NoError(t, db.First(&account, account.ID).Error)
			expected := 1
			if fullImport {
				expected = 2
			}
			assert.Equal(t, expected, account.DomainCount)
			assert.NotNil(t, account.LastSyncAt)
			var count int64
			require.NoError(t, db.Model(&domain.NormalizedDomain{}).Count(&count).Error)
			assert.EqualValues(t, expected, count)
			var log domain.SyncLog
			require.NoError(t, db.First(&log).Error)
			assert.Equal(t, "completed", log.Status)
			assert.Equal(t, expected, log.DomainsSynced)
		})
	}
}

func TestRunSyncCycle_MissingCryptoService(t *testing.T) {
	db := setupTestDB(t)
	config := domain.RegistrarConfig{RegistrarType: "cloudflare"}
	require.NoError(t, db.Create(&config).Error)
	account := domain.RegistrarAccount{RegistrarConfigID: config.ID, AccountName: "No key"}
	require.NoError(t, db.Create(&account).Error)
	scheduler := NewSyncScheduler(db, adapter.NewAdapterRegistry(), nil, zap.NewNop())
	require.ErrorContains(t, scheduler.RunSyncCycle(context.Background(), account.ID), "not configured")
	var log domain.SyncLog
	require.NoError(t, db.First(&log).Error)
	assert.Equal(t, "failed", log.Status)
}

func TestCheckAndScheduleAccounts_SkipsDisabledAndDisconnected(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	config := domain.RegistrarConfig{RegistrarType: "cloudflare"}
	require.NoError(t, db.Create(&config).Error)
	accounts := []domain.RegistrarAccount{
		{RegistrarConfigID: config.ID, AccountName: "Disabled", Status: "connected"},
		{RegistrarConfigID: config.ID, AccountName: "Disconnected", Status: "disconnected"},
		{RegistrarConfigID: config.ID, AccountName: "Enabled", Status: "connected"},
	}
	require.NoError(t, db.Create(&accounts).Error)
	require.NoError(t, db.Model(&accounts[0]).Update("auto_sync_enabled", false).Error)
	scheduler := NewSyncScheduler(db, adapter.NewAdapterRegistry(), nil, zap.NewNop())
	scheduler.checkAndScheduleAccounts(context.Background())
	assert.Len(t, scheduler.lastSyncTimes, 1)
	assert.Contains(t, scheduler.lastSyncTimes, accounts[2].ID)
	require.Eventually(t, func() bool {
		var count int64
		err := db.Model(&domain.SyncLog{}).
			Where("registrar_account_id = ? AND status = ?", accounts[2].ID, "failed").Count(&count).Error
		return err == nil && count == 1
	}, time.Second, 10*time.Millisecond)
	var count int64
	require.NoError(t, db.Model(&domain.SyncLog{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestSyncFailureAlert_CreatedAfterTwoConsecutiveFailures(t *testing.T) {
	db := setupTestDB(t)
	cryptoSvc, err := crypto.NewCryptoService("01234567890123456789012345678901")
	require.NoError(t, err)
	encrypted, err := cryptoSvc.Encrypt(`{"token":"test-token"}`)
	require.NoError(t, err)
	config := domain.RegistrarConfig{RegistrarType: "cloudflare", DisplayName: "Cloudflare"}
	require.NoError(t, db.Create(&config).Error)
	account := domain.RegistrarAccount{
		RegistrarConfigID: config.ID, AccountName: "flaky",
		CredentialsEncrypted: encrypted, Status: "connected", AutoSyncEnabled: false,
	}
	require.NoError(t, db.Create(&account).Error)

	// Adapter always fails.
	failing := &failingAdapter{registrarType: "cloudflare", listErr: assert.AnError}
	registry := adapter.NewAdapterRegistry()
	registry.Register(failing)
	var dispatched []domain.Alert
	scheduler := NewSyncScheduler(db, registry, cryptoSvc, zap.NewNop())
	scheduler.OnAlertCreated = func(a *domain.Alert) { dispatched = append(dispatched, *a) }

	// First failure: no alert yet (threshold is 2).
	require.Error(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	var count int64
	db.Model(&domain.Alert{}).Where("alert_type = ?", "sync_failure").Count(&count)
	assert.EqualValues(t, 0, count, "single failure must not alert")

	// Second consecutive failure: alert created and dispatched once.
	require.Error(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	var alerts []domain.Alert
	require.NoError(t, db.Where("alert_type = ?", "sync_failure").Find(&alerts).Error)
	require.Len(t, alerts, 1)
	assert.Equal(t, "warning", alerts[0].Severity)
	require.NotNil(t, alerts[0].RegistrarAccountID)
	assert.Equal(t, account.ID, *alerts[0].RegistrarAccountID)
	assert.False(t, alerts[0].Acknowledged)
	assert.Len(t, dispatched, 1, "webhook dispatch must fire exactly once")

	// Third failure: dedup — still only one alert.
	require.Error(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	db.Model(&domain.Alert{}).Where("alert_type = ?", "sync_failure").Count(&count)
	assert.EqualValues(t, 1, count, "duplicate alerts must be suppressed while open")
}

func TestSyncFailureAlert_AutoResolvedOnSuccess(t *testing.T) {
	db := setupTestDB(t)
	cryptoSvc, err := crypto.NewCryptoService("01234567890123456789012345678901")
	require.NoError(t, err)
	encrypted, err := cryptoSvc.Encrypt(`{"token":"test-token"}`)
	require.NoError(t, err)
	config := domain.RegistrarConfig{RegistrarType: "cloudflare", DisplayName: "Cloudflare"}
	require.NoError(t, db.Create(&config).Error)
	account := domain.RegistrarAccount{
		RegistrarConfigID: config.ID, AccountName: "recovering",
		CredentialsEncrypted: encrypted, Status: "connected", AutoSyncEnabled: false,
	}
	require.NoError(t, db.Create(&account).Error)

	failing := &failingAdapter{registrarType: "cloudflare", listErr: assert.AnError}
	registry := adapter.NewAdapterRegistry()
	registry.Register(failing)
	scheduler := NewSyncScheduler(db, registry, cryptoSvc, zap.NewNop())

	// Two failures raise the alert.
	require.Error(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	require.Error(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	var alert domain.Alert
	require.NoError(t, db.Where("alert_type = ?", "sync_failure").First(&alert).Error)

	// Adapter recovers; next cycle resolves the alert.
	registry.Register(&cycleAdapter{credentials: make(chan adapter.RegistrarCredential, 4)})
	require.NoError(t, scheduler.RunSyncCycle(context.Background(), account.ID))

	var got domain.Alert
	require.NoError(t, db.First(&got, alert.ID).Error)
	assert.True(t, got.Acknowledged, "successful sync must auto-resolve the sync_failure alert")
	require.NotNil(t, got.AcknowledgedAt)
}

// failingAdapter always fails ListDomains.
type failingAdapter struct {
	registrarType string
	listErr       error
}

func (a *failingAdapter) RegistrarType() string { return a.registrarType }
func (a *failingAdapter) TestConnection(context.Context, *adapter.RegistrarCredential) error {
	return nil
}
func (a *failingAdapter) GetDomainDetail(context.Context, *adapter.RegistrarCredential, string) (*domain.NormalizedDomain, error) {
	return nil, nil
}
func (a *failingAdapter) ListDomains(context.Context, *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	return nil, a.listErr
}

func TestRunSyncCycle_RecalculatesIntervalBothWays(t *testing.T) {
	db := setupTestDB(t)
	cryptoSvc, err := crypto.NewCryptoService("01234567890123456789012345678901")
	require.NoError(t, err)
	encrypted, err := cryptoSvc.Encrypt(`{"token":"test-token"}`)
	require.NoError(t, err)
	config := domain.RegistrarConfig{RegistrarType: "cloudflare", DisplayName: "Cloudflare"}
	require.NoError(t, db.Create(&config).Error)
	account := domain.RegistrarAccount{
		RegistrarConfigID: config.ID, AccountName: "adaptive",
		CredentialsEncrypted: encrypted, Status: "connected", AutoSyncEnabled: false,
	}
	require.NoError(t, db.Create(&account).Error)

	stub := &cycleAdapter{credentials: make(chan adapter.RegistrarCredential, 4)}
	registry := adapter.NewAdapterRegistry()
	registry.Register(stub)
	scheduler := NewSyncScheduler(db, registry, cryptoSvc, zap.NewNop())

	// Account's most imminent domain is 15 days out → 12h interval.
	near := time.Now().Add(15 * 24 * time.Hour)
	far := time.Now().Add(300 * 24 * time.Hour)
	stub.domains = []domain.NormalizedDomain{
		{DomainName: "near.test", ExpirationDate: &near, Status: "active"},
		{DomainName: "far.test", ExpirationDate: &far, Status: "active"},
	}
	require.NoError(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	var afterSpeedUp domain.RegistrarAccount
	require.NoError(t, db.First(&afterSpeedUp, account.ID).Error)
	assert.Equal(t, 12, afterSpeedUp.SyncIntervalHours, "imminent domain must speed the account up")

	// The imminent domain renews far out → interval must slow back down.
	renewed := time.Now().Add(400 * 24 * time.Hour)
	stub.domains = []domain.NormalizedDomain{
		{DomainName: "near.test", ExpirationDate: &renewed, Status: "active"},
		{DomainName: "far.test", ExpirationDate: &far, Status: "active"},
	}
	require.NoError(t, scheduler.RunSyncCycle(context.Background(), account.ID))
	var afterSlowDown domain.RegistrarAccount
	require.NoError(t, db.First(&afterSlowDown, account.ID).Error)
	assert.Equal(t, 168, afterSlowDown.SyncIntervalHours, "renewal must slow the account back down")
}

func TestIsDue_UsesLatestManualSync(t *testing.T) {
	now := time.Now()
	scheduler := NewSyncScheduler(nil, nil, nil, zap.NewNop())
	scheduler.lastSyncTimes[1] = now.Add(-25 * time.Hour)
	account := domain.RegistrarAccount{ID: 1, SyncIntervalHours: 24, LastSyncAt: &now}
	assert.False(t, scheduler.isDue(account))
	account.LastSyncAt = nil
	assert.True(t, scheduler.isDue(account))
}

func TestRegistrarSyncOptions_AutoMigrateDefaults(t *testing.T) {
	db := setupTestDB(t)
	config := domain.RegistrarConfig{RegistrarType: "cloudflare"}
	require.NoError(t, db.Create(&config).Error)
	account := domain.RegistrarAccount{RegistrarConfigID: config.ID, AccountName: "Legacy account"}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Migrator().DropColumn(&domain.RegistrarAccount{}, "AutoSyncEnabled"))
	require.NoError(t, db.Migrator().DropColumn(&domain.RegistrarAccount{}, "FullImportEnabled"))
	require.NoError(t, db.AutoMigrate(&domain.RegistrarAccount{}))
	var migrated domain.RegistrarAccount
	require.NoError(t, db.First(&migrated, account.ID).Error)
	assert.True(t, migrated.AutoSyncEnabled)
	assert.True(t, migrated.FullImportEnabled)
	require.NoError(t, db.Model(&migrated).Updates(map[string]interface{}{
		"auto_sync_enabled": false, "full_import_enabled": false,
	}).Error)
	require.NoError(t, db.AutoMigrate(&domain.RegistrarAccount{}))
	require.NoError(t, db.First(&migrated, account.ID).Error)
	assert.False(t, migrated.AutoSyncEnabled)
	assert.False(t, migrated.FullImportEnabled)
}
