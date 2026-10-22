package godaddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainradar/internal/adapter"

	"github.com/stretchr/testify/require"
)

func TestGetDomainDetail_OptionalFlags(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("flags="+value, func(t *testing.T) {
			body := `{"domain":"partial.test","status":"ACTIVE"`
			if value != "" {
				body += `,"renewAuto":` + value + `,"privacy":` + value + `,"locked":` + value
			}
			body += `}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			detail, err := New(WithBaseURL(server.URL)).GetDomainDetail(context.Background(), &adapter.RegistrarCredential{}, "partial.test")
			require.NoError(t, err)
			require.Equal(t, "partial.test", detail.DomainName)
			for _, flag := range []*bool{detail.AutoRenew, detail.PrivacyProtection, detail.LockStatus} {
				if value == "" {
					require.Nil(t, flag)
				} else {
					require.NotNil(t, flag)
					require.Equal(t, value == "true", *flag)
				}
			}
		})
	}
}

func TestListAndDetail_OptionalFields(t *testing.T) {
	for _, fields := range []string{
		"",
		`,"renewAuto":null,"privacy":null,"locked":null,"nameServers":null`,
		`,"renewAuto":false,"privacy":false,"locked":false,"nameServers":[]`,
		`,"renewAuto":true,"privacy":true,"locked":true,"nameServers":["ns1.example.com"]`,
	} {
		t.Run(fields, func(t *testing.T) {
			body := `{"domain":"partial.test"` + fields + `}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				result := body
				if r.URL.Path == "/domains" {
					result = "[" + body + "]"
				}
				_, _ = w.Write([]byte(result))
			}))
			defer server.Close()
			a := New(WithBaseURL(server.URL))
			cred := &adapter.RegistrarCredential{}
			list, err := a.ListDomains(context.Background(), cred)
			require.NoError(t, err)
			require.Len(t, list, 1)
			detail, err := a.GetDomainDetail(context.Background(), cred, "partial.test")
			require.NoError(t, err)
			list[0].LastSyncAt = nil
			detail.LastSyncAt = nil
			require.Equal(t, *detail, list[0])
			if fields == "" {
				require.Nil(t, list[0].AutoRenew)
				require.Nil(t, list[0].PrivacyProtection)
				require.Nil(t, list[0].LockStatus)
				require.Nil(t, list[0].Nameservers)
				require.Empty(t, list[0].Status)
			}
		})
	}
}
