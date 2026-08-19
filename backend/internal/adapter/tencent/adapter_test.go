package tencent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"domainradar/internal/adapter"
)

func TestRegistrarType(t *testing.T) {
	a := New()
	assert.Equal(t, "tencent", a.RegistrarType())
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "DescribeDomainNameList", r.Header.Get("X-TC-Action"))
		assert.Equal(t, "2018-02-08", r.Header.Get("X-TC-Version"))
		assert.Contains(t, r.Header.Get("Authorization"), "TC3-HMAC-SHA256")

		resp := describeDomainNameListResponse{}
		resp.Response.DomainList = []tencentDomainListItem{}
		resp.Response.TotalCount = 0
		resp.Response.RequestId = "test-request-id"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-secret-id",
		SecretAccessKey: "test-secret-key",
	}

	err := a.TestConnection(context.Background(), cred)
	assert.NoError(t, err)
}

func TestTestConnection_MissingCredentials(t *testing.T) {
	a := New()
	cred := &adapter.RegistrarCredential{}

	err := a.TestConnection(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing SecretId or SecretKey")
}

func TestTestConnection_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := describeDomainNameListResponse{}
		resp.Response.Error = &tencentAPIError{
			Code:    "AuthFailure.SecretIdNotFound",
			Message: "SecretId not found",
		}
		resp.Response.RequestId = "test-request-id"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "bad-id",
		SecretAccessKey: "bad-key",
	}

	err := a.TestConnection(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AuthFailure.SecretIdNotFound")
}

func TestTestConnection_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-id",
		SecretAccessKey: "test-key",
	}

	err := a.TestConnection(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestListDomains_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := describeDomainNameListResponse{}
		resp.Response.DomainList = []tencentDomainListItem{
			{
				DomainName:     "example.com",
				CreationDate:   "2020-01-15",
				ExpirationDate: "2025-01-15",
				BuyStatus:      3,
				IsAutoRenew:    true,
			},
			{
				DomainName:     "test.cn",
				CreationDate:   "2021-06-01",
				ExpirationDate: "2024-06-01",
				BuyStatus:      1,
				IsAutoRenew:    false,
			},
		}
		resp.Response.TotalCount = 2
		resp.Response.RequestId = "list-request-id"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-secret-id",
		SecretAccessKey: "test-secret-key",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	require.NoError(t, err)
	require.Len(t, domains, 2)

	// First domain
	assert.Equal(t, "example.com", domains[0].DomainName)
	assert.Equal(t, "tencent", domains[0].RegistrarIdentifier)
	assert.Equal(t, "api", domains[0].DataSourceType)
	assert.Equal(t, "active", domains[0].Status)
	assert.True(t, domains[0].AutoRenew)
	assert.NotNil(t, domains[0].CreationDate)
	assert.Equal(t, 2020, domains[0].CreationDate.Year())
	assert.NotNil(t, domains[0].ExpirationDate)
	assert.Equal(t, 2025, domains[0].ExpirationDate.Year())

	// Second domain
	assert.Equal(t, "test.cn", domains[1].DomainName)
	assert.Equal(t, "expired", domains[1].Status)
	assert.False(t, domains[1].AutoRenew)
}

func TestListDomains_Pagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := describeDomainNameListResponse{}
		resp.Response.TotalCount = 150
		resp.Response.RequestId = "page-request-id"

		if callCount == 1 {
			// First page: 100 domains
			for i := 0; i < 100; i++ {
				resp.Response.DomainList = append(resp.Response.DomainList, tencentDomainListItem{
					DomainName:     "domain" + string(rune('a'+i%26)) + ".com",
					CreationDate:   "2020-01-01",
					ExpirationDate: "2025-01-01",
					BuyStatus:      3,
				})
			}
		} else {
			// Second page: 50 domains
			for i := 0; i < 50; i++ {
				resp.Response.DomainList = append(resp.Response.DomainList, tencentDomainListItem{
					DomainName:     "extra" + string(rune('a'+i%26)) + ".com",
					CreationDate:   "2021-01-01",
					ExpirationDate: "2026-01-01",
					BuyStatus:      3,
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-id",
		SecretAccessKey: "test-key",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	require.NoError(t, err)
	assert.Equal(t, 150, len(domains))
	assert.Equal(t, 2, callCount)
}

func TestListDomains_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := describeDomainNameListResponse{}
		resp.Response.Error = &tencentAPIError{
			Code:    "InvalidParameter",
			Message: "Invalid parameter",
		}
		resp.Response.RequestId = "error-request-id"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-id",
		SecretAccessKey: "test-key",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	assert.Error(t, err)
	assert.Nil(t, domains)
	assert.Contains(t, err.Error(), "InvalidParameter")
}

func TestGetDomainDetail_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DescribeDomainBaseInfo", r.Header.Get("X-TC-Action"))

		resp := describeDomainBaseInfoResponse{}
		resp.Response.DomainInfo = tencentDomainBaseInfo{
			DomainName:     "example.com",
			CreationDate:   "2020-01-15",
			ExpirationDate: "2025-01-15",
			BuyStatus:      3,
			IsAutoRenew:    true,
			Nameservers:    []string{"ns1.dnspod.net", "ns2.dnspod.net"},
		}
		resp.Response.RequestId = "detail-request-id"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-id",
		SecretAccessKey: "test-key",
	}

	d, err := a.GetDomainDetail(context.Background(), cred, "example.com")
	require.NoError(t, err)
	require.NotNil(t, d)

	assert.Equal(t, "example.com", d.DomainName)
	assert.Equal(t, "tencent", d.RegistrarIdentifier)
	assert.Equal(t, "api", d.DataSourceType)
	assert.Equal(t, "active", d.Status)
	assert.True(t, d.AutoRenew)
	assert.NotNil(t, d.CreationDate)
	assert.NotNil(t, d.ExpirationDate)
	assert.Len(t, d.Nameservers, 2)
	assert.Equal(t, "ns1.dnspod.net", string(d.Nameservers[0]))
}

func TestGetDomainDetail_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := describeDomainBaseInfoResponse{}
		resp.Response.Error = &tencentAPIError{
			Code:    "ResourceNotFound.DomainNotFound",
			Message: "Domain not found",
		}
		resp.Response.RequestId = "error-request-id"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-id",
		SecretAccessKey: "test-key",
	}

	d, err := a.GetDomainDetail(context.Background(), cred, "nonexistent.com")
	assert.Error(t, err)
	assert.Nil(t, d)
	assert.Contains(t, err.Error(), "DomainNotFound")
}

func TestCredentialExtraction_ExtraFields(t *testing.T) {
	cred := &adapter.RegistrarCredential{
		Extra: map[string]string{
			"secret_id":  "extra-secret-id",
			"secret_key": "extra-secret-key",
		},
	}

	assert.Equal(t, "extra-secret-id", getSecretID(cred))
	assert.Equal(t, "extra-secret-key", getSecretKey(cred))
}

func TestCredentialExtraction_StandardFields(t *testing.T) {
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "standard-id",
		SecretAccessKey: "standard-key",
	}

	assert.Equal(t, "standard-id", getSecretID(cred))
	assert.Equal(t, "standard-key", getSecretKey(cred))
}

func TestCredentialExtraction_ExtraTakesPriority(t *testing.T) {
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "standard-id",
		SecretAccessKey: "standard-key",
		Extra: map[string]string{
			"secret_id": "extra-id",
		},
	}

	// Extra takes priority for secret_id
	assert.Equal(t, "extra-id", getSecretID(cred))
	// SecretAccessKey is preferred first for secret key
	assert.Equal(t, "standard-key", getSecretKey(cred))
}

func TestMapBuyStatus(t *testing.T) {
	tests := []struct {
		status   int
		expected string
	}{
		{1, "expired"},
		{2, "expired"},
		{3, "active"},
		{4, "pending_transfer"},
		{0, "active"},
		{99, "active"},
	}

	for _, tt := range tests {
		t.Run("status_"+string(rune('0'+tt.status)), func(t *testing.T) {
			assert.Equal(t, tt.expected, mapBuyStatus(tt.status))
		})
	}
}

func TestHostFromEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		expected string
	}{
		{"https://domain.tencentcloudapi.com", "domain.tencentcloudapi.com"},
		{"http://localhost:8080", "localhost:8080"},
		{"https://domain.tencentcloudapi.com/path", "domain.tencentcloudapi.com"},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			assert.Equal(t, tt.expected, hostFromEndpoint(tt.endpoint))
		})
	}
}

func TestSignatureHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify all required headers are present
		assert.NotEmpty(t, r.Header.Get("Authorization"))
		assert.NotEmpty(t, r.Header.Get("X-TC-Action"))
		assert.NotEmpty(t, r.Header.Get("X-TC-Version"))
		assert.NotEmpty(t, r.Header.Get("X-TC-Timestamp"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Verify authorization format
		auth := r.Header.Get("Authorization")
		assert.Contains(t, auth, "TC3-HMAC-SHA256")
		assert.Contains(t, auth, "Credential=")
		assert.Contains(t, auth, "SignedHeaders=content-type;host")
		assert.Contains(t, auth, "Signature=")

		resp := describeDomainNameListResponse{}
		resp.Response.TotalCount = 0
		resp.Response.RequestId = "sig-test"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "AKID12345",
		SecretAccessKey: "secret12345",
	}

	err := a.TestConnection(context.Background(), cred)
	assert.NoError(t, err)
}
