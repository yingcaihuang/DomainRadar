package cloudflare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainradar/internal/adapter"

	"github.com/stretchr/testify/require"
)

func boolPtr(value bool) *bool { return &value }

func TestGetDomainDetail_OptionalFlags(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("flags="+value, func(t *testing.T) {
			body := `{"success":true,"result":{"name":"partial.test"`
			if value != "" {
				body += `,"auto_renew":` + value + `,"locked":` + value + `,"name_servers":[]`
			}
			body += `}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			detail, err := NewWithOptions(server.URL, server.Client()).GetDomainDetail(context.Background(),
				&adapter.RegistrarCredential{Extra: map[string]string{"account_id": "test"}}, "partial.test")
			require.NoError(t, err)
			require.Nil(t, detail.PrivacyProtection)
			if value == "" {
				require.Nil(t, detail.AutoRenew)
				require.Nil(t, detail.LockStatus)
				require.Nil(t, detail.Nameservers)
			} else {
				require.NotNil(t, detail.AutoRenew)
				require.NotNil(t, detail.LockStatus)
				require.Equal(t, value == "true", *detail.AutoRenew)
				require.Equal(t, value == "true", *detail.LockStatus)
				require.NotNil(t, detail.Nameservers)
				require.Empty(t, detail.Nameservers)
			}
		})
	}
}

func TestGetDomainDetail_ExpiredStatus(t *testing.T) {
	body := `{"success":true,"result":{"name":"expired.test","expires_at":"2020-01-01T00:00:00Z"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	detail, err := NewWithOptions(server.URL, server.Client()).GetDomainDetail(context.Background(),
		&adapter.RegistrarCredential{Extra: map[string]string{"account_id": "test"}}, "expired.test")
	require.NoError(t, err)
	require.NotNil(t, detail.ExpirationDate)
	require.Equal(t, "expired", detail.Status)
}

func TestListAndDetail_OptionalFields(t *testing.T) {
	for _, fields := range []string{
		"",
		`,"auto_renew":null,"locked":null,"name_servers":null`,
		`,"auto_renew":false,"locked":false,"name_servers":[]`,
		`,"auto_renew":true,"locked":true,"name_servers":["ns1.example.com"]`,
	} {
		t.Run(fields, func(t *testing.T) {
			body := `{"name":"partial.test"` + fields + `}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := body
				if r.URL.Path == "/accounts/test/registrar/domains" {
					result = "[" + body + "]"
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"result":` + result + `}`))
			}))
			defer server.Close()
			a := NewWithOptions(server.URL, server.Client())
			cred := &adapter.RegistrarCredential{Extra: map[string]string{"account_id": "test"}}
			list, err := a.ListDomains(context.Background(), cred)
			require.NoError(t, err)
			require.Len(t, list, 1)
			detail, err := a.GetDomainDetail(context.Background(), cred, "partial.test")
			require.NoError(t, err)
			list[0].LastSyncAt = nil
			detail.LastSyncAt = nil
			require.Equal(t, *detail, list[0])
		})
	}
}
