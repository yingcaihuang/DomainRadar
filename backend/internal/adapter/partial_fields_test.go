package adapter

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"domainradar/internal/domain"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDomainDetail_UpdateFields(t *testing.T) {
	updates, err := (DomainDetail{}).UpdateFields()
	require.NoError(t, err)
	require.Empty(t, updates)

	disabled := false
	date := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	updates, err = (DomainDetail{
		AutoRenew: &disabled, LockStatus: &disabled, PrivacyProtection: &disabled,
		ExpirationDate: &date, CreationDate: &date, RenewalDeadline: &date,
		Status: "locked", Nameservers: domain.JSON{},
	}).UpdateFields()
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{
		"auto_renew": false, "lock_status": false, "privacy_protection": false,
		"expiration_date": &date, "creation_date": &date, "renewal_deadline": &date,
		"status": "locked", "nameservers": "[]",
	}, updates)
}

func setupPartialImport(t *testing.T, snapshot DomainDetail) (*testSetup, domain.RegistrarAccount) {
	t.Helper()
	ts := newTestSetup(t)
	require.NoError(t, ts.db.AutoMigrate(&domain.NormalizedDomain{}, &domain.Group{}, &domain.Tag{}))
	config := domain.RegistrarConfig{RegistrarType: "godaddy"}
	require.NoError(t, ts.db.Create(&config).Error)
	encrypted, err := ts.cryptoService.Encrypt(`{"api_key":"test"}`)
	require.NoError(t, err)
	account := domain.RegistrarAccount{RegistrarConfigID: config.ID, AccountName: "partial", CredentialsEncrypted: encrypted}
	require.NoError(t, ts.db.Create(&account).Error)
	ts.adapterRegistry.Register(&mockAdapter{registrarType: "godaddy", domains: []DomainDetail{snapshot}})
	ts.router.POST("/api/v1/registrars/:id/import", ts.handler.SelectiveImport)
	ts.router.POST("/api/v1/registrars/:id/preview-sync", ts.handler.PreviewSync)
	return ts, account
}

func TestSelectiveImport_PartialFields(t *testing.T) {
	disabled, enabled := false, true
	for _, tc := range []struct {
		name     string
		snapshot DomainDetail
		wantFlag bool
		wantNS   domain.JSON
	}{
		{name: "omitted", wantFlag: true, wantNS: domain.JSON{"ns1.local.test"}},
		{name: "explicit-false", snapshot: DomainDetail{AutoRenew: &disabled, LockStatus: &disabled, PrivacyProtection: &disabled, Nameservers: domain.JSON{}}, wantNS: domain.JSON{}},
		{name: "explicit-true", snapshot: DomainDetail{AutoRenew: &enabled, LockStatus: &enabled, PrivacyProtection: &enabled, Nameservers: domain.JSON{"ns1.remote.test"}}, wantFlag: true, wantNS: domain.JSON{"ns1.remote.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.snapshot.DomainName = "partial.test"
			ts, account := setupPartialImport(t, tc.snapshot)
			expiry := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
			created := expiry.AddDate(-5, 0, 0)
			renewal := expiry.AddDate(0, 0, -30)
			existing := domain.NormalizedDomain{
				DomainName: "partial.test", RegistrarAccountID: &account.ID,
				AutoRenew: true, LockStatus: true, PrivacyProtection: true,
				ExpirationDate: &expiry, CreationDate: &created, RenewalDeadline: &renewal,
				Status: "locked", Nameservers: domain.JSON{"ns1.local.test"}, Notes: "local notes", AbsenceCount: 1,
			}
			require.NoError(t, ts.db.Create(&existing).Error)
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/registrars/%d/import", account.ID),
				bytes.NewBufferString(`{"domain_names":["partial.test"]}`))
			request.Header.Set("Content-Type", "application/json")
			ts.router.ServeHTTP(w, request)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.JSONEq(t, `{"message":"import completed","imported":1,"total":1}`, w.Body.String())

			var saved domain.NormalizedDomain
			require.NoError(t, ts.db.First(&saved, existing.ID).Error)
			require.Equal(t, tc.wantFlag, saved.AutoRenew)
			require.Equal(t, tc.wantFlag, saved.LockStatus)
			require.Equal(t, tc.wantFlag, saved.PrivacyProtection)
			require.Equal(t, tc.wantNS, saved.Nameservers)
			require.True(t, expiry.Equal(*saved.ExpirationDate))
			require.True(t, created.Equal(*saved.CreationDate))
			require.True(t, renewal.Equal(*saved.RenewalDeadline))
			require.Equal(t, existing.Status, saved.Status)
			require.Equal(t, existing.Notes, saved.Notes)
			require.NotNil(t, saved.LastSyncAt)
			require.Zero(t, saved.AbsenceCount)
		})
	}
}

func TestSelectiveImport_UpdateFailure(t *testing.T) {
	ts, account := setupPartialImport(t, DomainDetail{DomainName: "partial.test"})
	require.NoError(t, ts.db.Create(&domain.NormalizedDomain{DomainName: "partial.test"}).Error)
	require.NoError(t, ts.db.Callback().Update().Before("gorm:update").Register("fail_domain_update", func(tx *gorm.DB) {
		if tx.Statement.Table == (domain.NormalizedDomain{}).TableName() {
			tx.AddError(errors.New("domain update failed"))
		}
	}))
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/registrars/%d/import", account.ID),
		bytes.NewBufferString(`{"domain_names":["partial.test"]}`))
	request.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, request)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestSelectiveImport_QueryFailure(t *testing.T) {
	ts, account := setupPartialImport(t, DomainDetail{DomainName: "partial.test"})
	require.NoError(t, ts.db.Migrator().DropTable(&domain.NormalizedDomain{}))
	w := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/registrars/%d/import", account.ID),
		bytes.NewBufferString(`{"domain_names":["partial.test"]}`))
	request.Header.Set("Content-Type", "application/json")
	ts.router.ServeHTTP(w, request)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestPreviewSync_OptionalAutoRenew(t *testing.T) {
	disabled, enabled := false, true
	for _, flag := range []*bool{nil, &disabled, &enabled} {
		ts, account := setupPartialImport(t, DomainDetail{DomainName: "partial.test", AutoRenew: flag})
		w := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/registrars/%d/preview-sync", account.ID), nil)
		ts.router.ServeHTTP(w, request)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.JSONEq(t, fmt.Sprintf(`{"data":[{"domain_name":"partial.test","status":"","expiration_date":null,"auto_renew":%t,"creation_date":null}],"total":1}`, flag != nil && *flag), w.Body.String())
	}
}
