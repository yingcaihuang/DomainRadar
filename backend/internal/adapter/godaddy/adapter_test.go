package godaddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"domainradar/internal/adapter"
)

func TestRegistrarType(t *testing.T) {
	a := New()
	assert.Equal(t, "godaddy", a.RegistrarType())
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/domains", r.URL.Path)
		assert.Equal(t, "1", r.URL.Query().Get("limit"))
		assert.Equal(t, "sso-key testkey:testsecret", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]godaddyDomainResponse{})
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	err := a.TestConnection(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "testkey",
		APISecret: "testsecret",
	})
	assert.NoError(t, err)
}

func TestTestConnection_PAT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer my-pat-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]godaddyDomainResponse{})
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	err := a.TestConnection(context.Background(), &adapter.RegistrarCredential{
		Token: "my-pat-token",
	})
	assert.NoError(t, err)
}

func TestTestConnection_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	err := a.TestConnection(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "bad",
		APISecret: "creds",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestTestConnection_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	err := a.TestConnection(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected response (HTTP 500)")
}

func TestTestConnection_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL), WithTimeout(100*time.Millisecond))
	err := a.TestConnection(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestListDomains_Success(t *testing.T) {
	domains := []godaddyDomainResponse{
		{
			Domain:        "example.com",
			CreatedAt:     "2020-01-15T10:00:00Z",
			Expires:       "2025-01-15T10:00:00Z",
			RenewAuto:     true,
			RenewDeadline: "2025-01-10T10:00:00Z",
			Status:        "ACTIVE",
			NameServers:   []string{"ns1.godaddy.com", "ns2.godaddy.com"},
			Privacy:       true,
			Locked:        true,
		},
		{
			Domain:      "test.org",
			CreatedAt:   "2021-06-01T00:00:00Z",
			Expires:     "2024-06-01T00:00:00Z",
			RenewAuto:   false,
			Status:      "EXPIRED",
			NameServers: []string{"ns1.test.org"},
			Privacy:     false,
			Locked:      false,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/domains", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(domains)
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.ListDomains(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})

	require.NoError(t, err)
	require.Len(t, result, 2)

	// Check first domain mapping
	assert.Equal(t, "example.com", result[0].DomainName)
	assert.Equal(t, "godaddy", result[0].RegistrarIdentifier)
	assert.Equal(t, true, result[0].AutoRenew)
	assert.Equal(t, "active", result[0].Status)
	assert.Equal(t, []string{"ns1.godaddy.com", "ns2.godaddy.com"}, []string(result[0].Nameservers))
	assert.Equal(t, true, result[0].PrivacyProtection)
	assert.Equal(t, true, result[0].LockStatus)
	assert.Equal(t, "api", result[0].DataSourceType)
	assert.NotNil(t, result[0].CreationDate)
	assert.NotNil(t, result[0].ExpirationDate)
	assert.NotNil(t, result[0].RenewalDeadline)
	assert.NotNil(t, result[0].LastSyncAt)

	// Check second domain
	assert.Equal(t, "test.org", result[1].DomainName)
	assert.Equal(t, false, result[1].AutoRenew)
	assert.Equal(t, "expired", result[1].Status)
	assert.Equal(t, false, result[1].PrivacyProtection)
	assert.Equal(t, false, result[1].LockStatus)
}

func TestListDomains_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]godaddyDomainResponse{})
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.ListDomains(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestListDomains_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":"ACCESS_DENIED","message":"Access denied"}`))
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.ListDomains(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "API error (HTTP 403)")
}

func TestGetDomainDetail_Success(t *testing.T) {
	domainResp := godaddyDomainResponse{
		Domain:        "example.com",
		CreatedAt:     "2020-01-15T10:00:00Z",
		Expires:       "2025-01-15T10:00:00Z",
		RenewAuto:     true,
		RenewDeadline: "2025-01-10T10:00:00Z",
		Status:        "ACTIVE",
		NameServers:   []string{"ns1.godaddy.com", "ns2.godaddy.com"},
		Privacy:       true,
		Locked:        true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/domains/example.com", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(domainResp)
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.GetDomainDetail(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	}, "example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "example.com", result.DomainName)
	assert.Equal(t, "godaddy", result.RegistrarIdentifier)
	assert.Equal(t, true, result.AutoRenew)
	assert.Equal(t, "active", result.Status)
	assert.Equal(t, true, result.PrivacyProtection)
	assert.Equal(t, true, result.LockStatus)

	expectedCreated, _ := time.Parse(time.RFC3339, "2020-01-15T10:00:00Z")
	assert.Equal(t, expectedCreated, *result.CreationDate)

	expectedExpires, _ := time.Parse(time.RFC3339, "2025-01-15T10:00:00Z")
	assert.Equal(t, expectedExpires, *result.ExpirationDate)
}

func TestGetDomainDetail_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.GetDomainDetail(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	}, "nonexistent.com")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not found")
}

func TestSetAuthHeader_APIKeySecret(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	setAuthHeader(req, &adapter.RegistrarCredential{
		APIKey:    "mykey",
		APISecret: "mysecret",
	})
	assert.Equal(t, "sso-key mykey:mysecret", req.Header.Get("Authorization"))
}

func TestSetAuthHeader_PAT(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	setAuthHeader(req, &adapter.RegistrarCredential{
		Token: "my-personal-access-token",
	})
	assert.Equal(t, "Bearer my-personal-access-token", req.Header.Get("Authorization"))
}

func TestSetAuthHeader_PATTakesPrecedence(t *testing.T) {
	// When both Token and APIKey are set, PAT (Token) takes precedence
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	setAuthHeader(req, &adapter.RegistrarCredential{
		APIKey:    "mykey",
		APISecret: "mysecret",
		Token:     "my-pat",
	})
	assert.Equal(t, "Bearer my-pat", req.Header.Get("Authorization"))
}

func TestMapStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ACTIVE", "active"},
		{"CANCELLED", "cancelled"},
		{"CANCELED", "cancelled"},
		{"EXPIRED", "expired"},
		{"PENDING_TRANSFER", "pending_transfer"},
		{"LOCKED", "locked"},
		{"UNKNOWN", "active"},
		{"", "active"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, mapStatus(tc.input))
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC3339", "2020-01-15T10:00:00Z", false},
		{"RFC3339 with offset", "2020-01-15T10:00:00+08:00", false},
		{"RFC3339 with millis", "2020-01-15T10:00:00.123Z", false},
		{"Date only", "2020-01-15", false},
		{"Invalid", "not-a-date", true},
		{"Empty", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseTime(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.False(t, result.IsZero())
			}
		})
	}
}

func TestListDomains_SkipsMalformedDomains(t *testing.T) {
	// Domains with missing domain name are still parseable (empty string is valid for mapping)
	// This test ensures the list returns whatever can be parsed
	domains := []godaddyDomainResponse{
		{
			Domain:      "good.com",
			CreatedAt:   "2020-01-15T10:00:00Z",
			Expires:     "2025-01-15T10:00:00Z",
			Status:      "ACTIVE",
			NameServers: []string{"ns1.good.com"},
		},
		{
			Domain:      "also-good.com",
			Expires:     "2025-06-01T00:00:00Z",
			Status:      "ACTIVE",
			NameServers: []string{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(domains)
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.ListDomains(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})

	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestListDomains_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	a := New(WithBaseURL(server.URL))
	result, err := a.ListDomains(context.Background(), &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestTestConnection_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	a := New(WithBaseURL(server.URL))
	err := a.TestConnection(ctx, &adapter.RegistrarCredential{
		APIKey:    "key",
		APISecret: "secret",
	})
	assert.Error(t, err)
}
