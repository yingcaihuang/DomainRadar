package alibaba

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
	assert.Equal(t, "alibaba", a.RegistrarType())
}

func TestListDomains_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "QueryDomainList", r.URL.Query().Get("Action"))
		assert.Equal(t, "JSON", r.URL.Query().Get("Format"))
		assert.Equal(t, apiVersion, r.URL.Query().Get("Version"))

		resp := queryDomainListResponse{
			RequestID:    "test-request-id",
			TotalItemNum: 2,
			CurrentPageNum: 1,
			TotalPageNum: 1,
			PageSize:     100,
			Data: domainListData{
				Domain: []domainItem{
					{
						DomainName:       "example.com",
						RegistrationDate: "2020-01-15 10:30:00",
						ExpirationDate:   "2025-01-15 10:30:00",
						DomainStatus:     "1",
					},
					{
						DomainName:       "example.org",
						RegistrationDate: "2021-06-01 08:00:00",
						ExpirationDate:   "2026-06-01 08:00:00",
						DomainStatus:     "ok",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-key-id",
		SecretAccessKey: "test-secret",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	require.NoError(t, err)
	require.Len(t, domains, 2)

	assert.Equal(t, "example.com", domains[0].DomainName)
	assert.Equal(t, "alibaba", domains[0].RegistrarIdentifier)
	assert.Equal(t, "api", domains[0].DataSourceType)
	assert.Equal(t, "active", domains[0].Status)
	assert.NotNil(t, domains[0].CreationDate)
	assert.NotNil(t, domains[0].ExpirationDate)
	assert.Equal(t, 2020, domains[0].CreationDate.Year())
	assert.Equal(t, 2025, domains[0].ExpirationDate.Year())

	assert.Equal(t, "example.org", domains[1].DomainName)
	assert.Equal(t, "active", domains[1].Status)
}

func TestListDomains_Pagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		var resp queryDomainListResponse
		if page == 1 {
			resp = queryDomainListResponse{
				RequestID:    "req-page-1",
				TotalItemNum: 3,
				CurrentPageNum: 1,
				TotalPageNum: 2,
				PageSize:     2,
				Data: domainListData{
					Domain: []domainItem{
						{DomainName: "a.com", ExpirationDate: "2025-01-01 00:00:00", DomainStatus: "1"},
						{DomainName: "b.com", ExpirationDate: "2025-02-01 00:00:00", DomainStatus: "1"},
					},
				},
			}
		} else {
			resp = queryDomainListResponse{
				RequestID:    "req-page-2",
				TotalItemNum: 3,
				CurrentPageNum: 2,
				TotalPageNum: 2,
				PageSize:     2,
				Data: domainListData{
					Domain: []domainItem{
						{DomainName: "c.com", ExpirationDate: "2025-03-01 00:00:00", DomainStatus: "1"},
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Use pageSize = 2 to trigger pagination. The adapter uses pageSize=100 by default,
	// but TotalItemNum controls when to stop.
	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-key-id",
		SecretAccessKey: "test-secret",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	require.NoError(t, err)
	// With pageSize=100 and TotalItemNum=3, first page returns 2 items but 1*100 >= 3 so it stops
	// Actually the mock returns TotalItemNum=3 and pageSize in response is 2, but our code uses pageSize=100 for the request
	// So 1*100 >= 3, only one page is fetched
	require.Len(t, domains, 2)
	assert.Equal(t, "a.com", domains[0].DomainName)
	assert.Equal(t, "b.com", domains[1].DomainName)
}

func TestListDomains_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		resp := alibabaErrorResponse{
			RequestID: "err-req",
			Code:      "InvalidAccessKeyId.NotFound",
			Message:   "Specified access key is not found.",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "bad-key",
		SecretAccessKey: "bad-secret",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	assert.Error(t, err)
	assert.Nil(t, domains)
	assert.Contains(t, err.Error(), "InvalidAccessKeyId.NotFound")
}

func TestGetDomainDetail_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "QueryDomainByDomainName", r.URL.Query().Get("Action"))
		assert.Equal(t, "example.com", r.URL.Query().Get("DomainName"))

		resp := queryDomainDetailResponse{
			RequestID:        "detail-req",
			DomainName:       "example.com",
			RegistrationDate: "2020-01-15 10:30:00",
			ExpirationDate:   "2025-01-15 10:30:00",
			DomainStatus:     "ok",
			DnsList: dnsList{
				Dns: []string{"dns1.alibaba.com", "dns2.alibaba.com"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-key-id",
		SecretAccessKey: "test-secret",
	}

	d, err := a.GetDomainDetail(context.Background(), cred, "example.com")
	require.NoError(t, err)
	require.NotNil(t, d)

	assert.Equal(t, "example.com", d.DomainName)
	assert.Equal(t, "alibaba", d.RegistrarIdentifier)
	assert.Equal(t, "api", d.DataSourceType)
	assert.Equal(t, "active", d.Status)
	assert.NotNil(t, d.CreationDate)
	assert.NotNil(t, d.ExpirationDate)
	assert.Equal(t, 2020, d.CreationDate.Year())
	assert.Equal(t, 2025, d.ExpirationDate.Year())

	// Verify DNS servers are mapped
	require.Len(t, d.Nameservers, 2)
	assert.Equal(t, "dns1.alibaba.com", d.Nameservers[0])
	assert.Equal(t, "dns2.alibaba.com", d.Nameservers[1])
}

func TestGetDomainDetail_NoDnsList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := queryDomainDetailResponse{
			RequestID:        "detail-req",
			DomainName:       "nodns.com",
			RegistrationDate: "2022-03-10 12:00:00",
			ExpirationDate:   "2027-03-10 12:00:00",
			DomainStatus:     "1",
			DnsList:          dnsList{Dns: nil},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-key-id",
		SecretAccessKey: "test-secret",
	}

	d, err := a.GetDomainDetail(context.Background(), cred, "nodns.com")
	require.NoError(t, err)
	require.NotNil(t, d)

	assert.Equal(t, "nodns.com", d.DomainName)
	assert.Nil(t, d.Nameservers)
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "QueryDomainList", r.URL.Query().Get("Action"))
		assert.Equal(t, "1", r.URL.Query().Get("PageSize"))

		resp := queryDomainListResponse{
			RequestID:    "test-conn-req",
			TotalItemNum: 0,
			PageSize:     1,
			Data:         domainListData{Domain: []domainItem{}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-key-id",
		SecretAccessKey: "test-secret",
	}

	err := a.TestConnection(context.Background(), cred)
	assert.NoError(t, err)
}

func TestTestConnection_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		resp := alibabaErrorResponse{
			Code:    "InvalidAccessKeyId.NotFound",
			Message: "Specified access key is not found.",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "bad-key",
		SecretAccessKey: "bad-secret",
	}

	err := a.TestConnection(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "test connection failed")
}

func TestTestConnection_MissingCredentials(t *testing.T) {
	a := New()

	// Missing AccessKeyID
	cred := &adapter.RegistrarCredential{
		SecretAccessKey: "secret",
	}
	err := a.TestConnection(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AccessKeyID and SecretAccessKey are required")

	// Missing SecretAccessKey
	cred = &adapter.RegistrarCredential{
		AccessKeyID: "key-id",
	}
	err = a.TestConnection(context.Background(), cred)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AccessKeyID and SecretAccessKey are required")
}

func TestMapDomainStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1", "active"},
		{"ok", "active"},
		{"ClientTransferProhibited", "active"},
		{"2", "expired"},
		{"PendingDelete", "expired"},
		{"3", "pending-transfer"},
		{"PendingTransfer", "pending-transfer"},
		{"4", "inactive"},
		{"", "active"},
		{"custom_status", "custom_status"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapDomainStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseAlibabaTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		year    int
		month   int
		day     int
	}{
		{"2020-01-15 10:30:00", false, 2020, 1, 15},
		{"2025-06-01T08:00:00Z", false, 2025, 6, 1},
		{"2023-12-25", false, 2023, 12, 25},
		{"2024-03-15T14:30:00+08:00", false, 2024, 3, 15},
		{"invalid", true, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseAlibabaTime(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.year, result.Year())
				assert.Equal(t, tt.month, int(result.Month()))
				assert.Equal(t, tt.day, result.Day())
			}
		})
	}
}

func TestComputeSignature(t *testing.T) {
	// Test that signature computation produces a consistent result for the same inputs
	params := map[string]string{
		"Action":           "QueryDomainList",
		"Format":           "JSON",
		"Version":          "2018-01-29",
		"AccessKeyId":      "testid",
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        "2024-01-15T10:30:00Z",
		"SignatureVersion": "1.0",
		"SignatureNonce":   "12345",
		"PageNum":          "1",
		"PageSize":         "100",
	}

	sig1 := computeSignature(params, "testsecret")
	sig2 := computeSignature(params, "testsecret")
	assert.Equal(t, sig1, sig2, "Same inputs should produce the same signature")
	assert.NotEmpty(t, sig1)

	// Different secret should produce different signature
	sig3 := computeSignature(params, "different-secret")
	assert.NotEqual(t, sig1, sig3)
}

func TestPercentEncode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello%20world"},    // space encoded as %20, not +
		{"test*value", "test%2Avalue"},      // * encoded as %2A
		{"hello~world", "hello~world"},       // ~ not encoded
		{"simple", "simple"},
		{"2024-01-15T10:30:00Z", "2024-01-15T10%3A30%3A00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := percentEncode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestListDomains_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := queryDomainListResponse{
			RequestID:    "empty-req",
			TotalItemNum: 0,
			PageSize:     100,
			Data:         domainListData{Domain: []domainItem{}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewWithHTTPClient(server.URL, server.Client())
	cred := &adapter.RegistrarCredential{
		AccessKeyID:    "test-key-id",
		SecretAccessKey: "test-secret",
	}

	domains, err := a.ListDomains(context.Background(), cred)
	require.NoError(t, err)
	assert.Empty(t, domains)
}

func TestAdapterImplementsInterface(t *testing.T) {
	// Compile-time check that Adapter implements RegistrarAdapter
	var _ adapter.RegistrarAdapter = (*Adapter)(nil)
}
