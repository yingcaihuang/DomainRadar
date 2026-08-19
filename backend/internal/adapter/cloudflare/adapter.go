package cloudflare

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
	defaultBaseURL = "https://api.cloudflare.com/client/v4"
	registrarType  = "cloudflare"
)

// Adapter implements adapter.RegistrarAdapter for Cloudflare.
type Adapter struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new Cloudflare adapter with the default base URL.
func New() *Adapter {
	return &Adapter{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewWithOptions creates a new Cloudflare adapter with custom options.
// This is useful for testing with a mock HTTP server.
func NewWithOptions(baseURL string, httpClient *http.Client) *Adapter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Adapter{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// RegistrarType returns the identifier for this adapter.
func (a *Adapter) RegistrarType() string {
	return registrarType
}

// TestConnection validates that the API token is working by verifying the token.
func (a *Adapter) TestConnection(ctx context.Context, credential *adapter.RegistrarCredential) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/user/tokens/verify", nil)
	if err != nil {
		return fmt.Errorf("cloudflare: failed to create request: %w", err)
	}
	a.setAuthHeader(req, credential)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare: token verification failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result apiResponse[tokenVerifyResult]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("cloudflare: failed to parse verify response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("cloudflare: token verification unsuccessful: %s", formatErrors(result.Errors))
	}

	return nil
}

// ListDomains retrieves all domains registered under the Cloudflare account.
// Uses the Registrar Domains API: GET /accounts/{account_id}/registrar/domains
func (a *Adapter) ListDomains(ctx context.Context, credential *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	accountID, err := a.getAccountID(credential)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/accounts/%s/registrar/domains", a.baseURL, accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: failed to create request: %w", err)
	}
	a.setAuthHeader(req, credential)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cloudflare: list domains failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result apiResponse[[]registrarDomain]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("cloudflare: failed to parse response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("cloudflare: list domains unsuccessful: %s", formatErrors(result.Errors))
	}

	domains := make([]domain.NormalizedDomain, 0, len(result.Result))
	now := time.Now()
	for _, d := range result.Result {
		domains = append(domains, mapToDomain(d, now))
	}

	return domains, nil
}

// GetDomainDetail retrieves detailed information for a specific domain.
// Uses: GET /accounts/{account_id}/registrar/domains/{domain_name}
func (a *Adapter) GetDomainDetail(ctx context.Context, credential *adapter.RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	accountID, err := a.getAccountID(credential)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/accounts/%s/registrar/domains/%s", a.baseURL, accountID, domainName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: failed to create request: %w", err)
	}
	a.setAuthHeader(req, credential)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cloudflare: get domain detail failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result apiResponse[registrarDomain]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("cloudflare: failed to parse response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("cloudflare: get domain detail unsuccessful: %s", formatErrors(result.Errors))
	}

	now := time.Now()
	normalized := mapToDomain(result.Result, now)
	return &normalized, nil
}

// setAuthHeader adds the Bearer token authorization header to the request.
func (a *Adapter) setAuthHeader(req *http.Request, credential *adapter.RegistrarCredential) {
	req.Header.Set("Authorization", "Bearer "+credential.Token)
	req.Header.Set("Content-Type", "application/json")
}

// getAccountID extracts the account_id from the credential's Extra map.
func (a *Adapter) getAccountID(credential *adapter.RegistrarCredential) (string, error) {
	if credential.Extra == nil {
		return "", fmt.Errorf("cloudflare: account_id is required in credential extras")
	}
	accountID, ok := credential.Extra["account_id"]
	if !ok || accountID == "" {
		return "", fmt.Errorf("cloudflare: account_id is required in credential extras")
	}
	return accountID, nil
}

// mapToDomain converts a Cloudflare registrar domain to a NormalizedDomain.
func mapToDomain(d registrarDomain, now time.Time) domain.NormalizedDomain {
	nd := domain.NormalizedDomain{
		DomainName:          d.DomainName,
		RegistrarIdentifier: "cloudflare",
		AutoRenew:           d.AutoRenew,
		LockStatus:          d.Locked,
		DataSourceType:      "api",
		LastSyncAt:          &now,
		Status:              "active",
	}

	if d.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, d.CreatedAt); err == nil {
			nd.CreationDate = &t
		}
	}

	if d.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, d.ExpiresAt); err == nil {
			nd.ExpirationDate = &t
		}
	}

	if len(d.NameServers) > 0 {
		nd.Nameservers = domain.JSON(d.NameServers)
	}

	// Map Cloudflare-specific statuses
	if d.Available {
		nd.Status = "available"
	}

	return nd
}

// --- API response types ---

// apiResponse is the generic Cloudflare API response envelope.
type apiResponse[T any] struct {
	Success  bool       `json:"success"`
	Errors   []apiError `json:"errors"`
	Messages []string   `json:"messages"`
	Result   T          `json:"result"`
}

// apiError represents an error entry in the Cloudflare API response.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// tokenVerifyResult is the result of GET /user/tokens/verify.
type tokenVerifyResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	NotBefore string `json:"not_before"`
	ExpiresOn string `json:"expires_on"`
}

// registrarDomain represents a domain from the Cloudflare Registrar API.
type registrarDomain struct {
	DomainName       string   `json:"name"`
	CurrentRegistrar string   `json:"current_registrar"`
	CreatedAt        string   `json:"created_at"`
	ExpiresAt        string   `json:"expires_at"`
	AutoRenew        bool     `json:"auto_renew"`
	Locked           bool     `json:"locked"`
	NameServers      []string `json:"name_servers"`
	Available        bool     `json:"available"`
}

// formatErrors converts a list of Cloudflare API errors into a readable string.
func formatErrors(errors []apiError) string {
	if len(errors) == 0 {
		return "unknown error"
	}
	msg := ""
	for i, e := range errors {
		if i > 0 {
			msg += "; "
		}
		msg += fmt.Sprintf("[%d] %s", e.Code, e.Message)
	}
	return msg
}
