package namecheap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainradar/internal/adapter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCredential() *adapter.RegistrarCredential {
	return &adapter.RegistrarCredential{
		APIKey:      "test-api-key",
		Username:    "testuser",
		IPWhitelist: "192.168.1.1",
	}
}

func TestRegistrarType(t *testing.T) {
	a := New()
	assert.Equal(t, "namecheap", a.RegistrarType())
}

func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "testuser", r.URL.Query().Get("ApiUser"))
		assert.Equal(t, "test-api-key", r.URL.Query().Get("ApiKey"))
		assert.Equal(t, "testuser", r.URL.Query().Get("UserName"))
		assert.Equal(t, "192.168.1.1", r.URL.Query().Get("ClientIp"))
		assert.Equal(t, "namecheap.domains.getList", r.URL.Query().Get("Command"))
		assert.Equal(t, "1", r.URL.Query().Get("PageSize"))

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testConnectionSuccessXML))
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	err := a.TestConnection(context.Background(), testCredential())
	require.NoError(t, err)
}

func TestTestConnection_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(apiErrorXML))
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	err := a.TestConnection(context.Background(), testCredential())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API Key is invalid")
}

func TestTestConnection_NetworkError(t *testing.T) {
	a := New(WithAPIURL("http://127.0.0.1:1"))
	err := a.TestConnection(context.Background(), testCredential())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection test failed")
}

func TestListDomains_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "namecheap.domains.getList", r.URL.Query().Get("Command"))

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(listDomainsSuccessXML))
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	domains, err := a.ListDomains(context.Background(), testCredential())
	require.NoError(t, err)
	require.Len(t, domains, 2)

	// First domain
	assert.Equal(t, "example.com", domains[0].DomainName)
	assert.Equal(t, "namecheap", domains[0].RegistrarIdentifier)
	assert.True(t, domains[0].AutoRenew)
	assert.True(t, domains[0].LockStatus)
	assert.True(t, domains[0].PrivacyProtection)
	assert.Equal(t, "api", domains[0].DataSourceType)
	require.NotNil(t, domains[0].CreationDate)
	assert.Equal(t, 2020, domains[0].CreationDate.Year())
	assert.Equal(t, 1, int(domains[0].CreationDate.Month()))
	assert.Equal(t, 15, domains[0].CreationDate.Day())
	require.NotNil(t, domains[0].ExpirationDate)
	assert.Equal(t, 2025, domains[0].ExpirationDate.Year())
	assert.Equal(t, 1, int(domains[0].ExpirationDate.Month()))
	assert.Equal(t, 15, domains[0].ExpirationDate.Day())

	// Second domain
	assert.Equal(t, "test.org", domains[1].DomainName)
	assert.False(t, domains[1].AutoRenew)
	assert.False(t, domains[1].LockStatus)
	assert.False(t, domains[1].PrivacyProtection)
}

func TestListDomains_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(apiErrorXML))
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	domains, err := a.ListDomains(context.Background(), testCredential())
	require.Error(t, err)
	assert.Nil(t, domains)
	assert.Contains(t, err.Error(), "API Key is invalid")
}

func TestListDomains_Pagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)

		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("Page"))
			_, _ = w.Write([]byte(listDomainsSuccessXML)) // 2 domains (less than pageSize=100, so pagination stops)
		}
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	domains, err := a.ListDomains(context.Background(), testCredential())
	require.NoError(t, err)
	assert.Len(t, domains, 2)
	assert.Equal(t, 1, callCount)
}

func TestGetDomainDetail_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "namecheap.domains.getInfo", r.URL.Query().Get("Command"))
		assert.Equal(t, "example.com", r.URL.Query().Get("DomainName"))

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(getDomainInfoSuccessXML))
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	d, err := a.GetDomainDetail(context.Background(), testCredential(), "example.com")
	require.NoError(t, err)
	require.NotNil(t, d)

	assert.Equal(t, "example.com", d.DomainName)
	assert.Equal(t, "namecheap", d.RegistrarIdentifier)
	assert.Equal(t, "api", d.DataSourceType)
	assert.Equal(t, "Ok", d.Status)
	assert.True(t, d.PrivacyProtection)
	require.NotNil(t, d.CreationDate)
	assert.Equal(t, 2020, d.CreationDate.Year())
	require.NotNil(t, d.ExpirationDate)
	assert.Equal(t, 2025, d.ExpirationDate.Year())
	require.NotNil(t, d.Nameservers)
	assert.Len(t, d.Nameservers, 2)
	assert.Equal(t, "ns1.example.com", d.Nameservers[0])
	assert.Equal(t, "ns2.example.com", d.Nameservers[1])
}

func TestGetDomainDetail_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(apiErrorXML))
	}))
	defer server.Close()

	a := New(WithAPIURL(server.URL))
	d, err := a.GetDomainDetail(context.Background(), testCredential(), "example.com")
	require.Error(t, err)
	assert.Nil(t, d)
	assert.Contains(t, err.Error(), "API Key is invalid")
}

func TestBuildBaseParams(t *testing.T) {
	a := New()
	cred := testCredential()
	params := a.buildBaseParams(cred)

	assert.Equal(t, "testuser", params.Get("ApiUser"))
	assert.Equal(t, "test-api-key", params.Get("ApiKey"))
	assert.Equal(t, "testuser", params.Get("UserName"))
	assert.Equal(t, "192.168.1.1", params.Get("ClientIp"))
}

func TestAdapterImplementsInterface(t *testing.T) {
	// Compile-time check that Adapter implements RegistrarAdapter.
	var _ adapter.RegistrarAdapter = (*Adapter)(nil)
}

// --- Test XML fixtures ---

const testConnectionSuccessXML = `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors/>
  <CommandResponse Type="namecheap.domains.getList">
    <DomainGetListResult>
      <Domain ID="123" Name="example.com" User="testuser" Created="01/15/2020" Expires="01/15/2025" IsExpired="false" IsLocked="true" AutoRenew="true" WhoisGuard="ENABLED" IsPremium="false"/>
    </DomainGetListResult>
  </CommandResponse>
</ApiResponse>`

const listDomainsSuccessXML = `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors/>
  <CommandResponse Type="namecheap.domains.getList">
    <DomainGetListResult>
      <Domain ID="123" Name="example.com" User="testuser" Created="01/15/2020" Expires="01/15/2025" IsExpired="false" IsLocked="true" AutoRenew="true" WhoisGuard="ENABLED" IsPremium="false"/>
      <Domain ID="456" Name="test.org" User="testuser" Created="03/20/2021" Expires="03/20/2026" IsExpired="false" IsLocked="false" AutoRenew="false" WhoisGuard="NOTPRESENT" IsPremium="false"/>
    </DomainGetListResult>
  </CommandResponse>
</ApiResponse>`

const getDomainInfoSuccessXML = `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="OK" xmlns="http://api.namecheap.com/xml.response">
  <Errors/>
  <CommandResponse Type="namecheap.domains.getInfo">
    <DomainGetInfoResult Status="Ok" ID="123" DomainName="example.com">
      <DomainDetails>
        <CreatedDate>01/15/2020</CreatedDate>
        <ExpiredDate>01/15/2025</ExpiredDate>
        <NumYears>5</NumYears>
      </DomainDetails>
      <DnsDetails ProviderType="CUSTOM">
        <Nameserver>ns1.example.com</Nameserver>
        <Nameserver>ns2.example.com</Nameserver>
      </DnsDetails>
      <Whoisguard Enabled="true"/>
    </DomainGetInfoResult>
  </CommandResponse>
</ApiResponse>`

const apiErrorXML = `<?xml version="1.0" encoding="utf-8"?>
<ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response">
  <Errors>
    <Error Number="1011150">API Key is invalid or API access has not been enabled</Error>
  </Errors>
  <CommandResponse/>
</ApiResponse>`
