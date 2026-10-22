package domainmgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"domainradar/internal/domain"
)

func setupWhoisSchedulerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.NormalizedDomain{}))
	return db
}

func TestRunChecks_SkipsRegistrarBoundDomains(t *testing.T) {
	db := setupWhoisSchedulerTestDB(t)

	accountID := uint(7)
	apiDomain := domain.NormalizedDomain{
		DomainName:         "api-owned.com",
		RegistrarAccountID: &accountID,
		Status:             "active",
	}
	manualDomain := domain.NormalizedDomain{
		DomainName: "manual.com",
		Status:     "active",
	}
	require.NoError(t, db.Create(&apiDomain).Error)
	require.NoError(t, db.Create(&manualDomain).Error)

	whoDatCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		whoDatCalls++
		renewed := time.Now().Add(200 * 24 * time.Hour).UTC().Format(time.RFC3339)
		_, _ = w.Write([]byte(`{"dates":{"expires":"` + renewed + `"}}`))
	}))
	defer server.Close()

	t.Setenv("WHO_DAT_URL", server.URL)

	s := NewWhoisScheduler(db, zap.NewNop())
	s.runChecks(context.Background())

	assert.Equal(t, 1, whoDatCalls, "only the manual domain must be checked")
	var api domain.NormalizedDomain
	require.NoError(t, db.Where("domain_name = ?", "api-owned.com").First(&api).Error)
	assert.Nil(t, api.WhoisLastCheckedAt, "registrar-bound domain must not be touched by WHOIS")
	assert.Nil(t, api.ExpirationDate, "registrar-bound domain must keep its data")
}

func TestCheckDomain_UpdatesManualDomainStatus(t *testing.T) {
	db := setupWhoisSchedulerTestDB(t)

	// Renewed per registry, still marked expired in DB.
	staleExpiry := time.Now().Add(-27 * 24 * time.Hour)
	renewed := domain.NormalizedDomain{
		DomainName:     "renewed-manual.com",
		ExpirationDate: &staleExpiry,
		Status:         "expired",
	}
	require.NoError(t, db.Create(&renewed).Error)

	// Expired per registry, still marked active in DB.
	futureExpiry := time.Now().Add(30 * 24 * time.Hour)
	expiredNow := domain.NormalizedDomain{
		DomainName:     "expired-manual.com",
		ExpirationDate: &futureExpiry,
		Status:         "active",
	}
	require.NoError(t, db.Create(&expiredNow).Error)

	newRenewedDate := time.Now().Add(340 * 24 * time.Hour).UTC().Format(time.RFC3339)
	newExpiredDate := time.Now().Add(-9 * 24 * time.Hour).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/renewed-manual.com" {
			_, _ = w.Write([]byte(`{"dates":{"expires":"` + newRenewedDate + `"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"dates":{"expires":"` + newExpiredDate + `"}}`))
	}))
	defer server.Close()

	s := NewWhoisScheduler(db, zap.NewNop())
	_, err := s.checkDomain(context.Background(), &renewed, server.URL)
	require.NoError(t, err)
	_, err = s.checkDomain(context.Background(), &expiredNow, server.URL)
	require.NoError(t, err)

	var gotRenewed domain.NormalizedDomain
	require.NoError(t, db.Where("domain_name = ?", "renewed-manual.com").First(&gotRenewed).Error)
	assert.Equal(t, "active", gotRenewed.Status, "future WHOIS expiry must heal expired status")

	var gotExpired domain.NormalizedDomain
	require.NoError(t, db.Where("domain_name = ?", "expired-manual.com").First(&gotExpired).Error)
	assert.Equal(t, "expired", gotExpired.Status, "past WHOIS expiry must mark active domains expired")
}

func TestCheckNow_SingleManualDomain(t *testing.T) {
	db := setupWhoisSchedulerTestDB(t)

	staleExpiry := time.Now().Add(-27 * 24 * time.Hour)
	d := domain.NormalizedDomain{
		DomainName:     "checknow.com",
		ExpirationDate: &staleExpiry,
		Status:         "expired",
	}
	require.NoError(t, db.Create(&d).Error)

	renewed := time.Now().Add(340 * 24 * time.Hour).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"domain":"checknow.com","dates":{"expires":"` + renewed + `"},"registrar":{"name":"GoDaddy.com, LLC"}}`))
	}))
	defer server.Close()

	t.Setenv("WHO_DAT_URL", server.URL)

	s := NewWhoisScheduler(db, zap.NewNop())
	whoisData, updated, err := s.CheckNow(context.Background(), d.ID)
	require.NoError(t, err)

	require.NotNil(t, whoisData)
	assert.Equal(t, "checknow.com", whoisData["domain"])
	require.NotNil(t, updated)
	assert.Equal(t, "active", updated.Status)
	require.NotNil(t, updated.ExpirationDate)
	assert.WithinDuration(t, time.Now().Add(340*24*time.Hour), *updated.ExpirationDate, time.Minute)
}

func TestCheckNow_RejectsRegistrarBoundDomain(t *testing.T) {
	db := setupWhoisSchedulerTestDB(t)

	accountID := uint(3)
	d := domain.NormalizedDomain{
		DomainName:         "bound.com",
		RegistrarAccountID: &accountID,
		Status:             "active",
	}
	require.NoError(t, db.Create(&d).Error)

	s := NewWhoisScheduler(db, zap.NewNop())
	_, _, err := s.CheckNow(context.Background(), d.ID)
	require.Error(t, err, "registrar-bound domains must not be refreshed via WHOIS")
}
