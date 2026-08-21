package namecheap

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"domainradar/internal/adapter"
	"domainradar/internal/domain"
)

const (
	defaultAPIURL = "https://api.namecheap.com/xml.response"
	dateFormat    = "01/02/2006"
)

// Adapter implements the RegistrarAdapter interface for Namecheap.
type Adapter struct {
	apiURL     string
	httpClient *http.Client
}

// Option configures the Namecheap adapter.
type Option func(*Adapter)

// WithAPIURL overrides the default Namecheap API URL (useful for testing).
func WithAPIURL(url string) Option {
	return func(a *Adapter) {
		a.apiURL = url
	}
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(a *Adapter) {
		a.httpClient = client
	}
}

// New creates a new Namecheap adapter.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		apiURL: defaultAPIURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// RegistrarType returns the identifier for the Namecheap adapter.
func (a *Adapter) RegistrarType() string {
	return "namecheap"
}

// TestConnection validates that the credentials are working by making a lightweight API call.
func (a *Adapter) TestConnection(ctx context.Context, credential *adapter.RegistrarCredential) error {
	params := a.buildBaseParams(credential)
	params.Set("Command", "namecheap.domains.getList")
	params.Set("PageSize", "1")

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := a.doRequest(ctx, params)
	if err != nil {
		return fmt.Errorf("namecheap connection test failed: %w", err)
	}

	if resp.Status == "ERROR" {
		return fmt.Errorf("namecheap API error: %s", resp.firstError())
	}

	return nil
}

// ListDomains retrieves all active domains from the Namecheap account.
func (a *Adapter) ListDomains(ctx context.Context, credential *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	var allDomains []domain.NormalizedDomain
	page := 1
	pageSize := 100

	for {
		params := a.buildBaseParams(credential)
		params.Set("Command", "namecheap.domains.getList")
		params.Set("Page", fmt.Sprintf("%d", page))
		params.Set("PageSize", fmt.Sprintf("%d", pageSize))

		resp, err := a.doRequest(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("namecheap list domains failed: %w", err)
		}

		if resp.Status == "ERROR" {
			return nil, fmt.Errorf("namecheap API error: %s", resp.firstError())
		}

		var result domainsGetListResult
		if err := xml.Unmarshal(resp.CommandResponse.InnerXML, &result); err != nil {
			return nil, fmt.Errorf("namecheap parse domains list failed: %w", err)
		}

		for _, d := range result.Domains {
			normalized := mapDomainToNormalized(d)
			allDomains = append(allDomains, normalized)
		}

		if len(result.Domains) < pageSize {
			break
		}
		page++
	}

	return allDomains, nil
}

// GetDomainDetail retrieves detailed info for a specific domain.
func (a *Adapter) GetDomainDetail(ctx context.Context, credential *adapter.RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	params := a.buildBaseParams(credential)
	params.Set("Command", "namecheap.domains.getInfo")
	params.Set("DomainName", domainName)

	resp, err := a.doRequest(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("namecheap get domain detail failed: %w", err)
	}

	if resp.Status == "ERROR" {
		return nil, fmt.Errorf("namecheap API error: %s", resp.firstError())
	}

	var result domainGetInfoResult
	if err := xml.Unmarshal(resp.CommandResponse.InnerXML, &result); err != nil {
		return nil, fmt.Errorf("namecheap parse domain info failed: %w", err)
	}

	normalized := mapDomainInfoToNormalized(result)
	return &normalized, nil
}

// buildBaseParams constructs the common query parameters for Namecheap API requests.
func (a *Adapter) buildBaseParams(credential *adapter.RegistrarCredential) url.Values {
	params := url.Values{}
	params.Set("ApiUser", credential.Username)
	params.Set("ApiKey", credential.APIKey)
	params.Set("UserName", credential.Username)
	params.Set("ClientIp", credential.IPWhitelist)
	return params
}

// doRequest performs an HTTP GET request to the Namecheap API and parses the XML response.
func (a *Adapter) doRequest(ctx context.Context, params url.Values) (*apiResponse, error) {
	reqURL := fmt.Sprintf("%s?%s", a.apiURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpResp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	var resp apiResponse
	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse XML response failed: %w", err)
	}

	return &resp, nil
}

// mapDomainToNormalized converts a Namecheap domain list item to a NormalizedDomain.
func mapDomainToNormalized(d namecheapDomain) domain.NormalizedDomain {
	nd := domain.NormalizedDomain{
		DomainName:          d.Name,
		RegistrarIdentifier: "namecheap",
		AutoRenew:           d.AutoRenew,
		LockStatus:          d.IsLocked,
		PrivacyProtection:   d.WhoisGuard == "ENABLED",
		DataSourceType:      "api",
		Status:              "active",
	}

	// Override status if expired
	if nd.ExpirationDate != nil && nd.ExpirationDate.Before(time.Now()) {
		nd.Status = "expired"
	}

	if created, err := time.Parse(dateFormat, d.Created); err == nil {
		nd.CreationDate = &created
	}
	if expires, err := time.Parse(dateFormat, d.Expires); err == nil {
		nd.ExpirationDate = &expires
	}

	now := time.Now()
	nd.LastSyncAt = &now

	return nd
}

// mapDomainInfoToNormalized converts a Namecheap domain info response to a NormalizedDomain.
func mapDomainInfoToNormalized(info domainGetInfoResult) domain.NormalizedDomain {
	nd := domain.NormalizedDomain{
		DomainName:          info.DomainName,
		RegistrarIdentifier: "namecheap",
		DataSourceType:      "api",
		Status:              info.Status,
	}

	if info.DomainDetails.CreatedDate != "" {
		if created, err := time.Parse(dateFormat, info.DomainDetails.CreatedDate); err == nil {
			nd.CreationDate = &created
		}
	}
	if info.DomainDetails.ExpiredDate != "" {
		if expires, err := time.Parse(dateFormat, info.DomainDetails.ExpiredDate); err == nil {
			nd.ExpirationDate = &expires
		}
	}

	// Nameservers
	if len(info.DNSDetails.Nameservers) > 0 {
		nd.Nameservers = domain.JSON(info.DNSDetails.Nameservers)
	}

	// WhoisGuard
	nd.PrivacyProtection = info.WhoisGuard.Enabled

	now := time.Now()
	nd.LastSyncAt = &now

	return nd
}

// XML response structures for the Namecheap API.

// apiResponse is the top-level XML envelope returned by Namecheap.
type apiResponse struct {
	XMLName         xml.Name        `xml:"ApiResponse"`
	Status          string          `xml:"Status,attr"`
	Errors          apiErrors       `xml:"Errors"`
	CommandResponse commandResponse `xml:"CommandResponse"`
}

func (r *apiResponse) firstError() string {
	if len(r.Errors.Errors) > 0 {
		return r.Errors.Errors[0].Message
	}
	return "unknown error"
}

type apiErrors struct {
	Errors []apiError `xml:"Error"`
}

type apiError struct {
	Number  string `xml:"Number,attr"`
	Message string `xml:",chardata"`
}

type commandResponse struct {
	Type     string `xml:"Type,attr"`
	InnerXML []byte `xml:",innerxml"`
}

// domainsGetListResult represents the parsed CommandResponse for namecheap.domains.getList.
type domainsGetListResult struct {
	XMLName xml.Name          `xml:"DomainGetListResult"`
	Domains []namecheapDomain `xml:"Domain"`
}

// namecheapDomain represents a single domain in the getList response.
type namecheapDomain struct {
	ID         string `xml:"ID,attr"`
	Name       string `xml:"Name,attr"`
	User       string `xml:"User,attr"`
	Created    string `xml:"Created,attr"`
	Expires    string `xml:"Expires,attr"`
	IsExpired  bool   `xml:"IsExpired,attr"`
	IsLocked   bool   `xml:"IsLocked,attr"`
	AutoRenew  bool   `xml:"AutoRenew,attr"`
	WhoisGuard string `xml:"WhoisGuard,attr"`
	IsPremium  bool   `xml:"IsPremium,attr"`
}

// domainGetInfoResult represents the parsed CommandResponse for namecheap.domains.getInfo.
type domainGetInfoResult struct {
	XMLName       xml.Name        `xml:"DomainGetInfoResult"`
	Status        string          `xml:"Status,attr"`
	DomainName    string          `xml:"DomainName,attr"`
	ID            string          `xml:"ID,attr"`
	DomainDetails domainDetails   `xml:"DomainDetails"`
	DNSDetails    dnsDetails      `xml:"DnsDetails"`
	WhoisGuard    whoisGuardInfo  `xml:"Whoisguard"`
}

type domainDetails struct {
	CreatedDate  string `xml:"CreatedDate"`
	ExpiredDate  string `xml:"ExpiredDate"`
	NumYears     int    `xml:"NumYears"`
}

type dnsDetails struct {
	ProviderType string   `xml:"ProviderType,attr"`
	Nameservers  []string `xml:"Nameserver"`
}

type whoisGuardInfo struct {
	Enabled bool `xml:"Enabled,attr"`
}
