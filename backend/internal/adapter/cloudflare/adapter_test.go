package cloudflare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"domainradar/internal/adapter"
)

func newTestCredential() *adapter.RegistrarCredential {
	return &adapter.RegistrarCredential{
		Token: "test-api-token",
		Extra: map[string]string{
			"account_id": "abc123",
		},
	}
}

func TestRegistrarType(t *testing.T) {
	a := New()
	assert.Equal(t, "cloudflare", a.RegistrarType())
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/tokens/verify", r.URL.Path)
		assert.Equal(t, "Bearer test-api-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"messages": [],
			"result": {
				"id": "tok-id-123",
				"status": "active",
				"not_before": "2024-01-01T00:00:00Z",
				"expires_on": "2025-01-01T00:00:00Z"
			}
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	err := a.TestConnection(context.Background(), newTestCredential())
	assert.NoError(t, err)
}

func TestTestConnection_InvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"Invalid API Token"}]}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	err := a.TestConnection(context.Background(), newTestCredential())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token verification failed")
}

func TestTestConnection_APIErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": false,
			"errors": [{"code": 9109, "message": "Token has been revoked"}],
			"messages": [],
			"result": {}
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	err := a.TestConnection(context.Background(), newTestCredential())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token verification unsuccessful")
	assert.Contains(t, err.Error(), "Token has been revoked")
}

func TestTestConnection_NetworkError(t *testing.T) {
	a := NewWithOptions("http://127.0.0.1:1", &http.Client{Timeout: 100 * time.Millisecond})
	err := a.TestConnection(context.Background(), newTestCredential())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestTestConnection_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	err := a.TestConnection(ctx, newTestCredential())
	assert.Error(t, err)
}

func TestListDomains_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts/abc123/registrar/domains", r.URL.Path)
		assert.Equal(t, "Bearer test-api-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"messages": [],
			"result": [
				{
					"name": "example.com",
					"current_registrar": "Cloudflare",
					"created_at": "2020-03-15T10:00:00Z",
					"expires_at": "2025-03-15T10:00:00Z",
					"auto_renew": true,
					"locked": true,
					"name_servers": ["ns1.cloudflare.com", "ns2.cloudflare.com"],
					"available": false
				},
				{
					"name": "test.org",
					"current_registrar": "Cloudflare",
					"created_at": "2021-06-01T00:00:00Z",
					"expires_at": "2024-06-01T00:00:00Z",
					"auto_renew": false,
					"locked": false,
					"name_servers": ["ns3.cloudflare.com", "ns4.cloudflare.com"],
					"available": false
				}
			]
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	domains, err := a.ListDomains(context.Background(), newTestCredential())
	require.NoError(t, err)
	require.Len(t, domains, 2)

	// Verify first domain mapping
	d1 := domains[0]
	assert.Equal(t, "example.com", d1.DomainName)
	assert.Equal(t, "cloudflare", d1.RegistrarIdentifier)
	assert.True(t, d1.AutoRenew)
	assert.True(t, d1.LockStatus)
	assert.Equal(t, "api", d1.DataSourceType)
	assert.Equal(t, "active", d1.Status)
	assert.NotNil(t, d1.CreationDate)
	assert.Equal(t, 2020, d1.CreationDate.Year())
	assert.NotNil(t, d1.ExpirationDate)
	assert.Equal(t, 2025, d1.ExpirationDate.Year())
	assert.Equal(t, []string{"ns1.cloudflare.com", "ns2.cloudflare.com"}, []string(d1.Nameservers))
	assert.NotNil(t, d1.LastSyncAt)

	// Verify second domain mapping
	d2 := domains[1]
	assert.Equal(t, "test.org", d2.DomainName)
	assert.False(t, d2.AutoRenew)
	assert.False(t, d2.LockStatus)
}

func TestListDomains_EmptyResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"messages": [],
			"result": []
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	domains, err := a.ListDomains(context.Background(), newTestCredential())
	require.NoError(t, err)
	assert.Empty(t, domains)
}

func TestListDomains_MissingAccountID(t *testing.T) {
	a := New()
	cred := &adapter.RegistrarCredential{
		Token: "test-token",
	}

	_, err := a.ListDomains(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account_id is required")
}

func TestListDomains_EmptyAccountID(t *testing.T) {
	a := New()
	cred := &adapter.RegistrarCredential{
		Token: "test-token",
		Extra: map[string]string{"account_id": ""},
	}

	_, err := a.ListDomains(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account_id is required")
}

func TestListDomains_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Forbidden"}]}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	_, err := a.ListDomains(context.Background(), newTestCredential())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list domains failed")
}

func TestListDomains_UnsuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": false,
			"errors": [{"code": 7003, "message": "No route for the URI"}],
			"messages": [],
			"result": null
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	_, err := a.ListDomains(context.Background(), newTestCredential())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "list domains unsuccessful")
}

func TestGetDomainDetail_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts/abc123/registrar/domains/example.com", r.URL.Path)
		assert.Equal(t, "Bearer test-api-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"messages": [],
			"result": {
				"name": "example.com",
				"current_registrar": "Cloudflare, Inc.",
				"created_at": "2020-03-15T10:00:00Z",
				"expires_at": "2025-03-15T10:00:00Z",
				"auto_renew": true,
				"locked": true,
				"name_servers": ["ns1.cloudflare.com", "ns2.cloudflare.com"],
				"available": false
			}
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	d, err := a.GetDomainDetail(context.Background(), newTestCredential(), "example.com")
	require.NoError(t, err)
	require.NotNil(t, d)

	assert.Equal(t, "example.com", d.DomainName)
	assert.Equal(t, "cloudflare", d.RegistrarIdentifier)
	assert.True(t, d.AutoRenew)
	assert.True(t, d.LockStatus)
	assert.Equal(t, "api", d.DataSourceType)
	assert.NotNil(t, d.ExpirationDate)
	assert.Equal(t, 2025, d.ExpirationDate.Year())
	assert.Equal(t, time.March, d.ExpirationDate.Month())
	assert.Equal(t, 15, d.ExpirationDate.Day())
}

func TestGetDomainDetail_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1001,"message":"Domain not found"}]}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	_, err := a.GetDomainDetail(context.Background(), newTestCredential(), "notfound.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get domain detail failed")
}

func TestGetDomainDetail_MissingAccountID(t *testing.T) {
	a := New()
	cred := &adapter.RegistrarCredential{Token: "test-token"}

	_, err := a.GetDomainDetail(context.Background(), cred, "example.com")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account_id is required")
}

func TestListDomains_PartialDates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"messages": [],
			"result": [
				{
					"name": "nodates.com",
					"current_registrar": "Cloudflare",
					"created_at": "",
					"expires_at": "",
					"auto_renew": false,
					"locked": false,
					"name_servers": [],
					"available": false
				}
			]
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	domains, err := a.ListDomains(context.Background(), newTestCredential())
	require.NoError(t, err)
	require.Len(t, domains, 1)

	d := domains[0]
	assert.Equal(t, "nodates.com", d.DomainName)
	assert.Nil(t, d.CreationDate)
	assert.Nil(t, d.ExpirationDate)
	assert.Nil(t, d.Nameservers)
}

func TestListDomains_AvailableDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"errors": [],
			"messages": [],
			"result": [
				{
					"name": "available.com",
					"current_registrar": "",
					"created_at": "",
					"expires_at": "",
					"auto_renew": false,
					"locked": false,
					"name_servers": [],
					"available": true
				}
			]
		}`))
	}))
	defer server.Close()

	a := NewWithOptions(server.URL, server.Client())
	domains, err := a.ListDomains(context.Background(), newTestCredential())
	require.NoError(t, err)
	require.Len(t, domains, 1)
	assert.Equal(t, "available", domains[0].Status)
}

func TestFormatErrors_Empty(t *testing.T) {
	assert.Equal(t, "unknown error", formatErrors(nil))
	assert.Equal(t, "unknown error", formatErrors([]apiError{}))
}

func TestFormatErrors_Single(t *testing.T) {
	errs := []apiError{{Code: 1000, Message: "Invalid token"}}
	assert.Equal(t, "[1000] Invalid token", formatErrors(errs))
}

func TestFormatErrors_Multiple(t *testing.T) {
	errs := []apiError{
		{Code: 1000, Message: "Error one"},
		{Code: 2000, Message: "Error two"},
	}
	assert.Equal(t, "[1000] Error one; [2000] Error two", formatErrors(errs))
}
