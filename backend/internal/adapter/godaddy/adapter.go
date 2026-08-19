package godaddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"domainradar/internal/adapter"
	"domainradar/internal/domain"
)

const (
	defaultBaseURL = "https://api.godaddy.com/v1"
	defaultTimeout = 30 * time.Second
)

// godaddyDomainResponse represents the domain object returned by the GoDaddy API.
type godaddyDomainResponse struct {
	Domain        string    `json:"domain"`
	CreatedAt     string    `json:"createdAt"`
	Expires       string    `json:"expires"`
	RenewAuto     bool      `json:"renewAuto"`
	RenewDeadline string    `json:"renewDeadline"`
	Status        string    `json:"status"`
	NameServers   []string  `json:"nameServers"`
	Privacy       bool      `json:"privacy"`
	Locked        bool      `json:"locked"`
}

// Adapter implements RegistrarAdapter for GoDaddy.
type Adapter struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures the GoDaddy adapter.
type Option func(*Adapter)

// WithBaseURL sets a custom API base URL (useful for testing).
func WithBaseURL(url string) Option {
	return func(a *Adapter) {
		a.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(a *Adapter) {
		a.httpClient = client
	}
}

// WithTimeout sets a custom timeout for the HTTP client.
func WithTimeout(timeout time.Duration) Option {
	return func(a *Adapter) {
		a.httpClient.Timeout = timeout
	}
}

// New creates a new GoDaddy adapter with the given options.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// RegistrarType returns the identifier for this adapter.
func (a *Adapter) RegistrarType() string {
	return "godaddy"
}

// TestConnection validates that credentials are working by making a lightweight API call.
func (a *Adapter) TestConnection(ctx context.Context, credential *adapter.RegistrarCredential) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/domains?limit=1", nil)
	if err != nil {
		return fmt.Errorf("godaddy: failed to create request: %w", err)
	}

	setAuthHeader(req, credential)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("godaddy: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("godaddy: authentication failed (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("godaddy: unexpected response (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListDomains retrieves all active domains from the GoDaddy account.
func (a *Adapter) ListDomains(ctx context.Context, credential *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/domains?limit=1000&statuses=ACTIVE", nil)
	if err != nil {
		return nil, fmt.Errorf("godaddy: failed to create request: %w", err)
	}

	setAuthHeader(req, credential)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("godaddy: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("godaddy: API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var gdDomains []godaddyDomainResponse
	if err := json.NewDecoder(resp.Body).Decode(&gdDomains); err != nil {
		return nil, fmt.Errorf("godaddy: failed to decode response: %w", err)
	}

	domains := make([]domain.NormalizedDomain, 0, len(gdDomains))
	for _, gd := range gdDomains {
		d, err := mapToDomain(gd)
		if err != nil {
			// Skip domains that fail to parse, but continue processing others
			continue
		}
		domains = append(domains, d)
	}

	return domains, nil
}

// GetDomainDetail retrieves detailed info for a specific domain.
func (a *Adapter) GetDomainDetail(ctx context.Context, credential *adapter.RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/domains/"+domainName, nil)
	if err != nil {
		return nil, fmt.Errorf("godaddy: failed to create request: %w", err)
	}

	setAuthHeader(req, credential)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("godaddy: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("godaddy: domain %q not found", domainName)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("godaddy: API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var gd godaddyDomainResponse
	if err := json.NewDecoder(resp.Body).Decode(&gd); err != nil {
		return nil, fmt.Errorf("godaddy: failed to decode response: %w", err)
	}

	d, err := mapToDomain(gd)
	if err != nil {
		return nil, fmt.Errorf("godaddy: failed to map domain %q: %w", domainName, err)
	}

	return &d, nil
}

// setAuthHeader applies the appropriate Authorization header based on credential type.
// If Token is set, PAT mode is used. Otherwise, API Key + Secret mode is used.
func setAuthHeader(req *http.Request, credential *adapter.RegistrarCredential) {
	if credential.Token != "" {
		req.Header.Set("Authorization", "Bearer "+credential.Token)
	} else {
		req.Header.Set("Authorization", "sso-key "+credential.APIKey+":"+credential.APISecret)
	}
}

// mapToDomain converts a GoDaddy API response to a NormalizedDomain.
func mapToDomain(gd godaddyDomainResponse) (domain.NormalizedDomain, error) {
	d := domain.NormalizedDomain{
		DomainName:          gd.Domain,
		RegistrarIdentifier: "godaddy",
		AutoRenew:           gd.RenewAuto,
		Status:              mapStatus(gd.Status),
		Nameservers:         domain.JSON(gd.NameServers),
		PrivacyProtection:   gd.Privacy,
		LockStatus:          gd.Locked,
		DataSourceType:      "api",
	}

	now := time.Now()
	d.LastSyncAt = &now

	if gd.CreatedAt != "" {
		t, err := parseTime(gd.CreatedAt)
		if err == nil {
			d.CreationDate = &t
		}
	}

	if gd.Expires != "" {
		t, err := parseTime(gd.Expires)
		if err == nil {
			d.ExpirationDate = &t
		}
	}

	if gd.RenewDeadline != "" {
		t, err := parseTime(gd.RenewDeadline)
		if err == nil {
			d.RenewalDeadline = &t
		}
	}

	return d, nil
}

// mapStatus maps GoDaddy domain status to our internal status representation.
func mapStatus(gdStatus string) string {
	switch gdStatus {
	case "ACTIVE":
		return "active"
	case "CANCELLED", "CANCELED":
		return "cancelled"
	case "EXPIRED":
		return "expired"
	case "PENDING_TRANSFER":
		return "pending_transfer"
	case "LOCKED":
		return "locked"
	default:
		return "active"
	}
}

// parseTime attempts to parse a time string using common GoDaddy API formats.
func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// Compile-time check that Adapter implements RegistrarAdapter.
var _ adapter.RegistrarAdapter = (*Adapter)(nil)
