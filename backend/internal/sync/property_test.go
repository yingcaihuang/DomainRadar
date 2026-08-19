package sync

import (
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"pgregory.net/rapid"
)

// setupPropertyTestDB creates an in-memory SQLite database for property tests.
func setupPropertyTestDB(t *testing.T) *gorm.DB {
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

// TestProperty1_SyncFrequencyTierAssignment verifies that CalculateSyncInterval
// returns the correct interval for all expiration distances:
//   - >90 days → weekly (168h)
//   - 30-90 days → daily (24h)
//   - <30 days → every 12h
//
// **Validates: Requirements 12.1, 12.2, 12.3**
func TestProperty1_SyncFrequencyTierAssignment(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of days from -365 to 730 (covers expired through far future).
		daysFromNow := rapid.Float64Range(-365, 730).Draw(t, "daysFromNow")
		expiresAt := time.Now().Add(time.Duration(daysFromNow*24) * time.Hour)

		interval := CalculateSyncInterval(expiresAt)

		// Calculate what the actual days until expiry would be.
		daysUntilExpiry := time.Until(expiresAt).Hours() / 24

		switch {
		case daysUntilExpiry > 90:
			if interval != WeeklyInterval {
				t.Fatalf("expected weekly (168h) for %.1f days until expiry, got %v", daysUntilExpiry, interval)
			}
		case daysUntilExpiry > 30:
			if interval != DailyInterval {
				t.Fatalf("expected daily (24h) for %.1f days until expiry, got %v", daysUntilExpiry, interval)
			}
		default:
			if interval != TwelveHInterval {
				t.Fatalf("expected 12h for %.1f days until expiry, got %v", daysUntilExpiry, interval)
			}
		}
	})
}

// TestProperty2_SyncIntervalOverrideClamping verifies that ClampInterval always
// returns a value within [1h, 720h] (30 days) for any input duration.
//
// **Validates: Requirements 12.4**
func TestProperty2_SyncIntervalOverrideClamping(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a wide range of durations including negative, zero, and very large values.
		// Use nanoseconds as the base unit to cover sub-hour and multi-year durations.
		nanos := rapid.Int64Range(-365*24*int64(time.Hour), 365*24*int64(time.Hour)).Draw(t, "nanos")
		input := time.Duration(nanos)

		result := ClampInterval(input)

		// Property: output is always >= MinInterval (1h)
		if result < MinInterval {
			t.Fatalf("clamped result %v is below minimum %v for input %v", result, MinInterval, input)
		}

		// Property: output is always <= MaxInterval (720h)
		if result > MaxInterval {
			t.Fatalf("clamped result %v exceeds maximum %v for input %v", result, MaxInterval, input)
		}

		// Property: if input is within bounds, output equals input
		if input >= MinInterval && input <= MaxInterval {
			if result != input {
				t.Fatalf("expected input %v to pass through unchanged, got %v", input, result)
			}
		}
	})
}

// TestProperty3_DomainDataMergePreservesUserFields verifies that when domains are
// merged with API data, user-defined fields (tags, notes, group) remain unchanged.
//
// **Validates: Requirements 1.5, 13.4**
func TestProperty3_DomainDataMergePreservesUserFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		db := setupPropertyTestDB(t)

		accountID := uint(1)

		// Generate random user-defined fields.
		notes := rapid.String().Draw(rt, "notes")
		groupName := rapid.StringMatching(`[a-z]{1,20}`).Draw(rt, "groupName")
		tagName := rapid.StringMatching(`[a-z]{1,20}`).Draw(rt, "tagName")

		// Create a group.
		group := domain.Group{Name: groupName, Level: 1}
		if err := db.Create(&group).Error; err != nil {
			rt.Fatalf("failed to create group: %v", err)
		}

		// Create a tag.
		tag := domain.Tag{Name: tagName}
		if err := db.Create(&tag).Error; err != nil {
			rt.Fatalf("failed to create tag: %v", err)
		}

		// Create an existing domain with user-defined fields.
		oldExpiry := time.Now().Add(90 * 24 * time.Hour)
		existing := domain.NormalizedDomain{
			DomainName:         "property-test.com",
			RegistrarAccountID: &accountID,
			ExpirationDate:     &oldExpiry,
			Status:             "active",
			DataSourceType:     "api",
			GroupID:            &group.ID,
			Notes:              notes,
		}
		if err := db.Create(&existing).Error; err != nil {
			rt.Fatalf("failed to create existing domain: %v", err)
		}

		// Associate the tag.
		if err := db.Model(&existing).Association("Tags").Append(&tag); err != nil {
			rt.Fatalf("failed to associate tag: %v", err)
		}

		// Generate random API update data.
		newAutoRenew := rapid.Bool().Draw(rt, "autoRenew")
		newPrivacy := rapid.Bool().Draw(rt, "privacy")
		newLock := rapid.Bool().Draw(rt, "lock")
		daysAhead := rapid.IntRange(1, 365).Draw(rt, "daysAhead")
		newExpiry := time.Now().Add(time.Duration(daysAhead) * 24 * time.Hour)

		remoteDomains := []domain.NormalizedDomain{
			{
				DomainName:        "property-test.com",
				ExpirationDate:    &newExpiry,
				AutoRenew:         newAutoRenew,
				Status:            "active",
				PrivacyProtection: newPrivacy,
				LockStatus:        newLock,
			},
		}

		_, _, err := MergeDomains(db, accountID, remoteDomains, nil)
		if err != nil {
			rt.Fatalf("MergeDomains failed: %v", err)
		}

		// Reload and verify user-defined fields are preserved.
		var reloaded domain.NormalizedDomain
		if err := db.Preload("Tags").First(&reloaded, existing.ID).Error; err != nil {
			rt.Fatalf("failed to reload domain: %v", err)
		}

		// Property: Notes unchanged after merge.
		if reloaded.Notes != notes {
			rt.Fatalf("notes changed after merge: got %q, want %q", reloaded.Notes, notes)
		}

		// Property: GroupID unchanged after merge.
		if reloaded.GroupID == nil || *reloaded.GroupID != group.ID {
			rt.Fatalf("group_id changed after merge: got %v, want %d", reloaded.GroupID, group.ID)
		}

		// Property: Tags unchanged after merge.
		if len(reloaded.Tags) != 1 || reloaded.Tags[0].Name != tagName {
			rt.Fatalf("tags changed after merge: got %v, want [{Name: %q}]", reloaded.Tags, tagName)
		}
	})
}

// TestProperty4_DomainRemovalRequiresTwoConsecutiveAbsences verifies that domains
// are only marked "unverified-removed" after exactly 2 consecutive absences. A single
// absence increments the counter but does not change status.
//
// **Validates: Requirements 1.8**
func TestProperty4_DomainRemovalRequiresTwoConsecutiveAbsences(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		db := setupPropertyTestDB(t)

		accountID := uint(1)

		// Generate a random initial absence count from 0 to 5.
		initialAbsenceCount := rapid.IntRange(0, 5).Draw(rt, "initialAbsenceCount")
		expiry := time.Now().Add(60 * 24 * time.Hour)

		// Only test domains that haven't already been marked removed.
		// (TrackAbsences skips "unverified-removed" domains.)
		initialStatus := "active"

		d := domain.NormalizedDomain{
			DomainName:         "absence-test.com",
			RegistrarAccountID: &accountID,
			ExpirationDate:     &expiry,
			Status:             initialStatus,
			DataSourceType:     "api",
			AbsenceCount:       initialAbsenceCount,
		}
		if err := db.Create(&d).Error; err != nil {
			rt.Fatalf("failed to create domain: %v", err)
		}

		// Track absences with the domain NOT in the present list.
		err := TrackAbsences(db, accountID, []string{}, nil)
		if err != nil {
			rt.Fatalf("TrackAbsences failed: %v", err)
		}

		// Reload and check.
		var reloaded domain.NormalizedDomain
		if err := db.First(&reloaded, d.ID).Error; err != nil {
			rt.Fatalf("failed to reload domain: %v", err)
		}

		expectedAbsenceCount := initialAbsenceCount + 1

		// Property: absence count is incremented by 1.
		if reloaded.AbsenceCount != expectedAbsenceCount {
			rt.Fatalf("expected absence_count=%d, got %d", expectedAbsenceCount, reloaded.AbsenceCount)
		}

		// Property: status changes to "unverified-removed" iff new absence count >= 2.
		if expectedAbsenceCount >= 2 {
			if reloaded.Status != "unverified-removed" {
				rt.Fatalf("expected status 'unverified-removed' when absence_count=%d, got %q",
					expectedAbsenceCount, reloaded.Status)
			}
		} else {
			if reloaded.Status != initialStatus {
				rt.Fatalf("expected status unchanged (%q) when absence_count=%d, got %q",
					initialStatus, expectedAbsenceCount, reloaded.Status)
			}
		}
	})
}

// TestProperty31_SyncErrorResilience verifies that CalculateSyncInterval and
// ClampInterval never panic on edge cases (very large, very small, negative, zero inputs).
// This property covers sync error resilience for pure functions.
//
// **Validates: Requirements 1.7, 2.5**
func TestProperty31_SyncErrorResilience(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Test CalculateSyncInterval with extreme time values.
		// Generate years offset between -100 and +100 from now.
		yearsOffset := rapid.Float64Range(-100, 100).Draw(t, "yearsOffset")
		expiresAt := time.Now().Add(time.Duration(yearsOffset*365.25*24) * time.Hour)

		// Property: CalculateSyncInterval never panics and always returns a valid positive duration.
		interval := CalculateSyncInterval(expiresAt)
		if interval <= 0 {
			t.Fatalf("CalculateSyncInterval returned non-positive duration %v for input %v", interval, expiresAt)
		}

		// Property: result is always one of the three defined tiers.
		if interval != WeeklyInterval && interval != DailyInterval && interval != TwelveHInterval {
			t.Fatalf("CalculateSyncInterval returned unexpected interval %v (not 168h, 24h, or 12h)", interval)
		}

		// Test ClampInterval with extreme duration values.
		extremeNanos := rapid.Int64().Draw(t, "extremeDuration")
		extremeDuration := time.Duration(extremeNanos)

		// Property: ClampInterval never panics and always returns a value in [1h, 720h].
		clamped := ClampInterval(extremeDuration)
		if clamped < MinInterval {
			t.Fatalf("ClampInterval returned %v < MinInterval for input %v", clamped, extremeDuration)
		}
		if clamped > MaxInterval {
			t.Fatalf("ClampInterval returned %v > MaxInterval for input %v", clamped, extremeDuration)
		}
	})
}
