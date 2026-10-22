package namecheap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func boolPtr(value bool) *bool { return &value }

func TestGetDomainDetail_OptionalFlags(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("privacy="+value, func(t *testing.T) {
			body := `<ApiResponse Status="OK"><CommandResponse><DomainGetInfoResult Status="Ok" DomainName="partial.test">`
			if value != "" {
				body += `<Whoisguard Enabled="` + value + `"/>`
			}
			body += `</DomainGetInfoResult></CommandResponse></ApiResponse>`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			detail, err := New(WithAPIURL(server.URL)).GetDomainDetail(context.Background(), testCredential(), "partial.test")
			require.NoError(t, err)
			require.Nil(t, detail.AutoRenew)
			require.Nil(t, detail.LockStatus)
			if value == "" {
				require.Nil(t, detail.PrivacyProtection)
			} else {
				require.NotNil(t, detail.PrivacyProtection)
				require.Equal(t, value == "true", *detail.PrivacyProtection)
			}
		})
	}
}

func TestListDomains_OptionalFlags(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("flags="+value, func(t *testing.T) {
			attrs := ""
			if value != "" {
				privacy := "DISABLED"
				if value == "true" {
					privacy = "ENABLED"
				}
				attrs = ` AutoRenew="` + value + `" IsLocked="` + value + `" WhoisGuard="` + privacy + `"`
			}
			body := `<ApiResponse Status="OK"><CommandResponse><DomainGetListResult><Domain Name="partial.test"` + attrs + `/></DomainGetListResult></CommandResponse></ApiResponse>`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			domains, err := New(WithAPIURL(server.URL)).ListDomains(context.Background(), testCredential())
			require.NoError(t, err)
			require.Len(t, domains, 1)
			for _, flag := range []*bool{domains[0].AutoRenew, domains[0].LockStatus, domains[0].PrivacyProtection} {
				if value == "" {
					require.Nil(t, flag)
				} else {
					require.NotNil(t, flag)
					require.Equal(t, value == "true", *flag)
				}
			}
			require.Nil(t, domains[0].Nameservers)
			require.Empty(t, domains[0].Status)
		})
	}
}

func TestGetDomainDetail_OptionalNameservers(t *testing.T) {
	for _, dns := range []string{"", `<DnsDetails/>`, `<DnsDetails><Nameserver>ns1.example.com</Nameserver></DnsDetails>`} {
		t.Run(dns, func(t *testing.T) {
			body := `<ApiResponse Status="OK"><CommandResponse><DomainGetInfoResult DomainName="partial.test">` + dns + `</DomainGetInfoResult></CommandResponse></ApiResponse>`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			detail, err := New(WithAPIURL(server.URL)).GetDomainDetail(context.Background(), testCredential(), "partial.test")
			require.NoError(t, err)
			switch dns {
			case "":
				require.Nil(t, detail.Nameservers)
			case `<DnsDetails/>`:
				require.NotNil(t, detail.Nameservers)
				require.Empty(t, detail.Nameservers)
			default:
				require.Equal(t, []string{"ns1.example.com"}, []string(detail.Nameservers))
			}
		})
	}
}
