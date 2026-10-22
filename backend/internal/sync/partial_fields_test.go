package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"domainradar/internal/adapter"
	"domainradar/internal/adapter/cloudflare"
	"domainradar/internal/crypto"
	"domainradar/internal/domain"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRunSyncCycle_CloudflarePartialFields(t *testing.T) {
	for _, fullImport := range []bool{false, true} {
		mode := "existing-only"
		if fullImport {
			mode = "full-import"
		}
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				fields      string
				enabled     bool
				nameservers domain.JSON
			}{
				{name: "omitted", enabled: true, nameservers: domain.JSON{"ns1.local.test"}},
				{name: "null", fields: `,"auto_renew":null,"locked":null,"name_servers":null,"expires_at":null,"created_at":null`, enabled: true, nameservers: domain.JSON{"ns1.local.test"}},
				{name: "false-and-empty", fields: `,"auto_renew":false,"locked":false,"name_servers":[]`, nameservers: domain.JSON{}},
				{name: "true-and-nonempty", fields: `,"auto_renew":true,"locked":true,"name_servers":["ns1.remote.test"]`, enabled: true, nameservers: domain.JSON{"ns1.remote.test"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					db := setupTestDB(t)
					cryptoSvc, err := crypto.NewCryptoService("01234567890123456789012345678901")
					require.NoError(t, err)
					encrypted, err := cryptoSvc.Encrypt(`{"token":"test-token","account_id":"cf-account"}`)
					require.NoError(t, err)
					config := domain.RegistrarConfig{RegistrarType: "cloudflare"}
					require.NoError(t, db.Create(&config).Error)
					account := domain.RegistrarAccount{RegistrarConfigID: config.ID, AccountName: tc.name, CredentialsEncrypted: encrypted}
					require.NoError(t, db.Create(&account).Error)
					require.NoError(t, db.Model(&account).Update("full_import_enabled", fullImport).Error)
					expiry := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
					created := expiry.AddDate(-5, 0, 0)
					renewal := expiry.AddDate(0, 0, -30)
					oldSync := time.Now().Add(-time.Hour)
					existing := domain.NormalizedDomain{
						DomainName: "partial.test", RegistrarAccountID: &account.ID, DataSourceType: "api",
						AutoRenew: true, LockStatus: true, PrivacyProtection: true,
						ExpirationDate: &expiry, CreationDate: &created, RenewalDeadline: &renewal,
						Status: "locked", Nameservers: domain.JSON{"ns1.local.test"},
						Notes: "keep local notes", AbsenceCount: 1, LastSyncAt: &oldSync,
					}
					require.NoError(t, db.Create(&existing).Error)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/accounts/cf-account/registrar/domains", r.URL.Path)
						require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"success":true,"result":[{"name":"partial.test"` + tc.fields + `}]}`))
					}))
					defer server.Close()
					registry := adapter.NewAdapterRegistry()
					registry.Register(cloudflare.NewWithOptions(server.URL, server.Client()))
					scheduler := NewSyncScheduler(db, registry, cryptoSvc, zap.NewNop())

					require.NoError(t, scheduler.RunSyncCycle(context.Background(), account.ID))

					var saved domain.NormalizedDomain
					require.NoError(t, db.First(&saved, existing.ID).Error)
					require.Equal(t, tc.enabled, saved.AutoRenew)
					require.Equal(t, tc.enabled, saved.LockStatus)
					require.True(t, saved.PrivacyProtection, "Cloudflare does not report privacy protection")
					require.Equal(t, tc.nameservers, saved.Nameservers)
					require.True(t, expiry.Equal(*saved.ExpirationDate))
					require.True(t, created.Equal(*saved.CreationDate))
					require.True(t, renewal.Equal(*saved.RenewalDeadline))
					require.Equal(t, "locked", saved.Status)
					require.Equal(t, existing.Notes, saved.Notes)
					require.Zero(t, saved.AbsenceCount)
					require.NotNil(t, saved.LastSyncAt)
					require.True(t, saved.LastSyncAt.After(oldSync))
					var log domain.SyncLog
					require.NoError(t, db.First(&log).Error)
					require.Equal(t, "completed", log.Status)
					require.Equal(t, 1, log.DomainsUpdated)
				})
			}
		})
	}
}

func TestMergeDomains_ProvidedFields(t *testing.T) {
	db := setupTestDB(t)
	existing := domain.NormalizedDomain{DomainName: "partial.test"}
	require.NoError(t, db.Create(&existing).Error)
	expiry := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	created := expiry.AddDate(-5, 0, 0)
	renewal := expiry.AddDate(0, 0, -30)
	_, updated, err := MergeDomains(db, 1, []adapter.DomainDetail{{
		DomainName: "partial.test", AutoRenew: boolPtr(true), LockStatus: boolPtr(true), PrivacyProtection: boolPtr(true),
		ExpirationDate: &expiry, CreationDate: &created, RenewalDeadline: &renewal,
		Status: "locked", Nameservers: domain.JSON{"ns1.remote.test"},
	}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	var saved domain.NormalizedDomain
	require.NoError(t, db.First(&saved, existing.ID).Error)
	require.True(t, saved.AutoRenew)
	require.True(t, saved.LockStatus)
	require.True(t, saved.PrivacyProtection)
	require.True(t, expiry.Equal(*saved.ExpirationDate))
	require.True(t, created.Equal(*saved.CreationDate))
	require.True(t, renewal.Equal(*saved.RenewalDeadline))
	require.Equal(t, "locked", saved.Status)
	require.Equal(t, domain.JSON{"ns1.remote.test"}, saved.Nameservers)
}

func TestMergeDomains_PartialNewDomainDefaults(t *testing.T) {
	db := setupTestDB(t)
	synced, updated, err := MergeDomains(db, 1, []adapter.DomainDetail{{DomainName: "new.test"}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, synced)
	require.Equal(t, 1, updated)
	var saved domain.NormalizedDomain
	require.NoError(t, db.First(&saved).Error)
	require.False(t, saved.AutoRenew)
	require.False(t, saved.LockStatus)
	require.False(t, saved.PrivacyProtection)
	require.Nil(t, saved.Nameservers)
	require.Equal(t, "active", saved.Status)
}
