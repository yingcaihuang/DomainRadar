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
