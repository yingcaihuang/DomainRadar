package sync

import (
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database with auto-migrated tables.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "failed to open in-memory sqlite")

	err = db.AutoMigrate(
		&domain.NormalizedDomain{},
		&domain.RegistrarAccount{},
		&domain.RegistrarConfig{},
		&domain.Group{},
		&domain.Tag{},
	)
	require.NoError(t, err, "failed to auto-migrate models")

	return db
}

// newTestLogger creates a no-op logger for testing.
func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// helper to create a time pointer.
func timePtr(t time.Time) *time.Time {
	return &t
}

// helper to create a uint pointer.
func uintPtr(v uint) *uint {
	return &v
}

func TestMergeDomains_CreatesNewDomains(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	remoteDomains := []domain.NormalizedDomain{
		{
			DomainName:     "example.com",
			ExpirationDate: &expiry,
			AutoRenew:      true,
			Status:         "active",
			Nameservers:    domain.JSON{"ns1.example.com", "ns2.example.com"},
		},
		{
			DomainName:     "test.org",
			ExpirationDate: &expiry,
			AutoRenew:      false,
			Status:         "active",
		},
	}

	synced, updated, err := MergeDomains(db, accountID, remoteDomains, logger)

	assert.NoError(t, err)
	assert.Equal(t, 2, synced)
	assert.Equal(t, 2, updated)

	// Verify domains were created in DB.
	var domains []domain.NormalizedDomain
	db.Find(&domains)
	assert.Len(t, domains, 2)

	// Verify data_source_type is "api" and registrar_account_id is set.
	for _, d := range domains {
		assert.Equal(t, "api", d.DataSourceType)
		assert.NotNil(t, d.RegistrarAccountID)
		assert.Equal(t, accountID, *d.RegistrarAccountID)
		assert.Equal(t, 0, d.AbsenceCount)
		assert.NotNil(t, d.LastSyncAt)
	}
}

func TestMergeDomains_UpdatesExistingDomain_PreservesUserFields(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	groupID := uint(5)
	oldExpiry := time.Now().Add(90 * 24 * time.Hour)

	// Create an existing domain with user-defined fields.
	existing := domain.NormalizedDomain{
		DomainName:         "existing.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &oldExpiry,
		AutoRenew:          false,
		Status:             "active",
		DataSourceType:     "api",
		GroupID:            &groupID,
		Notes:              "important domain for production",
	}
	require.NoError(t, db.Create(&existing).Error)

	// Also create a tag and associate it.
	tag := domain.Tag{Name: "production"}
	require.NoError(t, db.Create(&tag).Error)
	require.NoError(t, db.Model(&existing).Association("Tags").Append(&tag))

	// Now merge with updated API data.
	newExpiry := time.Now().Add(365 * 24 * time.Hour)
	remoteDomains := []domain.NormalizedDomain{
		{
			DomainName:        "existing.com",
			ExpirationDate:    &newExpiry,
			AutoRenew:         true,
			Status:            "active",
			Nameservers:       domain.JSON{"ns1.new.com"},
			PrivacyProtection: true,
			LockStatus:        true,
		},
	}

	synced, updated, err := MergeDomains(db, accountID, remoteDomains, logger)

	assert.NoError(t, err)
	assert.Equal(t, 1, synced)
	assert.Equal(t, 1, updated)

	// Reload the domain from DB.
	var reloaded domain.NormalizedDomain
	db.Preload("Tags").First(&reloaded, existing.ID)

	// API-sourced fields should be updated.
	assert.True(t, reloaded.AutoRenew)
	assert.True(t, reloaded.PrivacyProtection)
	assert.True(t, reloaded.LockStatus)
	assert.NotNil(t, reloaded.LastSyncAt)

	// User-defined fields should be preserved.
	assert.Equal(t, &groupID, reloaded.GroupID, "group_id should be preserved")
	assert.Equal(t, "important domain for production", reloaded.Notes, "notes should be preserved")
	assert.Len(t, reloaded.Tags, 1, "tags should be preserved")
	assert.Equal(t, "production", reloaded.Tags[0].Name)
}

func TestMergeDomains_ResetsAbsenceCountForPresentDomains(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	// Create a domain that had 1 absence.
	existing := domain.NormalizedDomain{
		DomainName:         "comeback.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       1,
	}
	require.NoError(t, db.Create(&existing).Error)

	// Merge with the domain present.
	remoteDomains := []domain.NormalizedDomain{
		{
			DomainName:     "comeback.com",
			ExpirationDate: &expiry,
			Status:         "active",
		},
	}

	_, _, err := MergeDomains(db, accountID, remoteDomains, logger)
	assert.NoError(t, err)

	// Verify absence count was reset.
	var reloaded domain.NormalizedDomain
	db.First(&reloaded, existing.ID)
	assert.Equal(t, 0, reloaded.AbsenceCount)
}

func TestTrackAbsences_IncrementsAbsenceCount(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	// Create a domain that is present and one that will be absent.
	present := domain.NormalizedDomain{
		DomainName:         "present.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       0,
	}
	absent := domain.NormalizedDomain{
		DomainName:         "absent.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       0,
	}
	require.NoError(t, db.Create(&present).Error)
	require.NoError(t, db.Create(&absent).Error)

	// Track absences with only "present.com" in the list.
	err := TrackAbsences(db, accountID, []string{"present.com"}, logger)
	assert.NoError(t, err)

	// "absent.com" should have absence count incremented.
	var reloadedAbsent domain.NormalizedDomain
	db.First(&reloadedAbsent, absent.ID)
	assert.Equal(t, 1, reloadedAbsent.AbsenceCount)
	assert.Equal(t, "active", reloadedAbsent.Status, "should not be marked unverified-removed after 1 absence")

	// "present.com" should be unchanged.
	var reloadedPresent domain.NormalizedDomain
	db.First(&reloadedPresent, present.ID)
	assert.Equal(t, 0, reloadedPresent.AbsenceCount)
}

func TestTrackAbsences_MarksUnverifiedRemovedAfterTwoAbsences(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	// Create a domain already with 1 absence.
	d := domain.NormalizedDomain{
		DomainName:         "disappearing.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       1,
	}
	require.NoError(t, db.Create(&d).Error)

	// Track absences again with the domain not present.
	err := TrackAbsences(db, accountID, []string{}, logger)
	assert.NoError(t, err)

	// Domain should now be marked "unverified-removed".
	var reloaded domain.NormalizedDomain
	db.First(&reloaded, d.ID)
	assert.Equal(t, 2, reloaded.AbsenceCount)
	assert.Equal(t, "unverified-removed", reloaded.Status)
}

func TestTrackAbsences_DoesNotReprocessAlreadyRemovedDomains(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	// Create a domain already marked as "unverified-removed".
	d := domain.NormalizedDomain{
		DomainName:         "already-removed.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &expiry,
		Status:             "unverified-removed",
		DataSourceType:     "api",
		AbsenceCount:       2,
	}
	require.NoError(t, db.Create(&d).Error)

	// Track absences — should not further modify the domain.
	err := TrackAbsences(db, accountID, []string{}, logger)
	assert.NoError(t, err)

	var reloaded domain.NormalizedDomain
	db.First(&reloaded, d.ID)
	assert.Equal(t, 2, reloaded.AbsenceCount, "should not increment further")
	assert.Equal(t, "unverified-removed", reloaded.Status)
}

func TestTrackAbsences_OnlyTracksDomainsForGivenAccount(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID1 := uint(1)
	accountID2 := uint(2)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	// Create domains for two different accounts.
	d1 := domain.NormalizedDomain{
		DomainName:         "account1-domain.com",
		RegistrarAccountID: &accountID1,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       0,
	}
	d2 := domain.NormalizedDomain{
		DomainName:         "account2-domain.com",
		RegistrarAccountID: &accountID2,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       0,
	}
	require.NoError(t, db.Create(&d1).Error)
	require.NoError(t, db.Create(&d2).Error)

	// Track absences for account 1 only (empty present list).
	err := TrackAbsences(db, accountID1, []string{}, logger)
	assert.NoError(t, err)

	// Account 1's domain should have absence incremented.
	var reloaded1 domain.NormalizedDomain
	db.First(&reloaded1, d1.ID)
	assert.Equal(t, 1, reloaded1.AbsenceCount)

	// Account 2's domain should be untouched.
	var reloaded2 domain.NormalizedDomain
	db.First(&reloaded2, d2.ID)
	assert.Equal(t, 0, reloaded2.AbsenceCount)
}

func TestDetectExpirationChange_BothNil(t *testing.T) {
	assert.False(t, DetectExpirationChange(nil, nil), "both nil should not be a change")
}

func TestDetectExpirationChange_OldNilNewNotNil(t *testing.T) {
	now := time.Now()
	assert.True(t, DetectExpirationChange(nil, &now), "nil->non-nil should be a change")
}

func TestDetectExpirationChange_OldNotNilNewNil(t *testing.T) {
	now := time.Now()
	assert.True(t, DetectExpirationChange(&now, nil), "non-nil->nil should be a change")
}

func TestDetectExpirationChange_SameTime(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	assert.False(t, DetectExpirationChange(&now, &now), "same time should not be a change")
}

func TestDetectExpirationChange_DifferentTime(t *testing.T) {
	old := time.Now()
	new := old.Add(24 * time.Hour)
	assert.True(t, DetectExpirationChange(&old, &new), "different times should be a change")
}

func TestDetectExpirationChange_SubSecondDifference(t *testing.T) {
	// Sub-second differences should be ignored.
	base := time.Now().Truncate(time.Second)
	slightlyDifferent := base.Add(500 * time.Millisecond)
	assert.False(t, DetectExpirationChange(&base, &slightlyDifferent), "sub-second difference should not be a change")
}

func TestMergeDomains_ExpirationChangeTriggersRecalculation(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)

	// Create registrar config and account for the sync interval update test.
	config := domain.RegistrarConfig{
		RegistrarType: "godaddy",
		DisplayName:   "GoDaddy",
	}
	require.NoError(t, db.Create(&config).Error)

	account := domain.RegistrarAccount{
		ID:                accountID,
		RegistrarConfigID: config.ID,
		AccountName:       "test-account",
		Status:            "connected",
		SyncIntervalHours: 168, // weekly
	}
	require.NoError(t, db.Create(&account).Error)

	// Create existing domain with far-off expiry (would be weekly sync).
	oldExpiry := time.Now().Add(120 * 24 * time.Hour)
	existing := domain.NormalizedDomain{
		DomainName:         "renewal.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &oldExpiry,
		Status:             "active",
		DataSourceType:     "api",
	}
	require.NoError(t, db.Create(&existing).Error)

	// Merge with new expiry that is within 30 days (should trigger 12h interval).
	newExpiry := time.Now().Add(15 * 24 * time.Hour)
	remoteDomains := []domain.NormalizedDomain{
		{
			DomainName:     "renewal.com",
			ExpirationDate: &newExpiry,
			Status:         "active",
		},
	}

	_, _, err := MergeDomains(db, accountID, remoteDomains, logger)
	assert.NoError(t, err)

	// Verify the account's sync interval was updated to 12h.
	var reloadedAccount domain.RegistrarAccount
	db.First(&reloadedAccount, accountID)
	assert.Equal(t, 12, reloadedAccount.SyncIntervalHours, "sync interval should be recalculated to 12h")
}

func TestMergeDomains_EmptyRemoteList(t *testing.T) {
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	// Create an existing domain.
	existing := domain.NormalizedDomain{
		DomainName:         "willbeabsent.com",
		RegistrarAccountID: &accountID,
		ExpirationDate:     &expiry,
		Status:             "active",
		DataSourceType:     "api",
		AbsenceCount:       0,
	}
	require.NoError(t, db.Create(&existing).Error)

	// Merge with empty remote list.
	synced, updated, err := MergeDomains(db, accountID, []domain.NormalizedDomain{}, logger)
	assert.NoError(t, err)
	assert.Equal(t, 0, synced)
	assert.Equal(t, 0, updated)

	// The existing domain should have absence incremented.
	var reloaded domain.NormalizedDomain
	db.First(&reloaded, existing.ID)
	assert.Equal(t, 1, reloaded.AbsenceCount)
}

func TestMergeDomains_LogsSyncOperation(t *testing.T) {
	// This test verifies that MergeDomains completes without error and
	// returns correct counts, which confirms logging paths are exercised.
	db := setupTestDB(t)
	logger := newTestLogger()

	accountID := uint(1)
	expiry := time.Now().Add(60 * 24 * time.Hour)

	remoteDomains := []domain.NormalizedDomain{
		{DomainName: "log-test-1.com", ExpirationDate: &expiry, Status: "active"},
		{DomainName: "log-test-2.com", ExpirationDate: &expiry, Status: "active"},
	}

	synced, updated, err := MergeDomains(db, accountID, remoteDomains, logger)
	assert.NoError(t, err)
	assert.Equal(t, 2, synced)
	assert.Equal(t, 2, updated)
}
