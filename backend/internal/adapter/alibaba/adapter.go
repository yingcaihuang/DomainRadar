package alibaba

import (
	"context"
	"fmt"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	domainSvc "github.com/alibabacloud-go/domain-20180129/v4/client"
	"github.com/alibabacloud-go/tea/tea"

	"domainradar/internal/adapter"
	"domainradar/internal/domain"
)

// Adapter implements RegistrarAdapter for Alibaba Cloud Domain using the official SDK.
type Adapter struct{}

// New creates a new Alibaba Cloud domain adapter.
func New() *Adapter {
	return &Adapter{}
}

// RegistrarType returns "alibaba".
func (a *Adapter) RegistrarType() string {
	return "alibaba"
}

// TestConnection validates credentials by calling QueryDomainList with PageSize=1.
func (a *Adapter) TestConnection(ctx context.Context, credential *adapter.RegistrarCredential) error {
	client, err := a.newClient(credential)
	if err != nil {
		return fmt.Errorf("alibaba: failed to create client: %w", err)
	}

	request := &domainSvc.QueryDomainListRequest{
		PageNum:  tea.Int32(1),
		PageSize: tea.Int32(1),
	}

	_, err = client.QueryDomainList(request)
	if err != nil {
		return fmt.Errorf("alibaba: %v", err)
	}

	return nil
}

// ListDomains retrieves all domains from the Alibaba Cloud account.
func (a *Adapter) ListDomains(ctx context.Context, credential *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	client, err := a.newClient(credential)
	if err != nil {
		return nil, fmt.Errorf("alibaba: failed to create client: %w", err)
	}

	var allDomains []domain.NormalizedDomain
	pageNum := int32(1)
	pageSize := int32(100)

	for {
		request := &domainSvc.QueryDomainListRequest{
			PageNum:  &pageNum,
			PageSize: &pageSize,
		}

		response, err := client.QueryDomainList(request)
		if err != nil {
			return nil, fmt.Errorf("alibaba: %v", err)
		}

		if response.Body == nil || response.Body.Data == nil || response.Body.Data.Domain == nil {
			break
		}

		for _, d := range response.Body.Data.Domain {
			nd := mapDomainItem(d)
			allDomains = append(allDomains, nd)
		}

		// Check if there are more pages
		if response.Body.NextPage == nil || !*response.Body.NextPage {
			break
		}
		pageNum++
	}

	return allDomains, nil
}

// GetDomainDetail retrieves detailed info for a specific domain.
func (a *Adapter) GetDomainDetail(ctx context.Context, credential *adapter.RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	client, err := a.newClient(credential)
	if err != nil {
		return nil, fmt.Errorf("alibaba: failed to create client: %w", err)
	}

	request := &domainSvc.QueryDomainByDomainNameRequest{
		DomainName: &domainName,
	}

	response, err := client.QueryDomainByDomainName(request)
	if err != nil {
		return nil, fmt.Errorf("alibaba: %v", err)
	}

	if response.Body == nil {
		return nil, fmt.Errorf("alibaba: empty response for domain %s", domainName)
	}

	nd := mapDomainDetail(response.Body)
	return &nd, nil
}

// newClient creates an Alibaba Cloud Domain API client from credentials.
func (a *Adapter) newClient(credential *adapter.RegistrarCredential) (*domainSvc.Client, error) {
	accessKeyID := credential.AccessKeyID
	accessKeySecret := credential.SecretAccessKey

	if accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("missing AccessKeyId or AccessKeySecret")
	}

	config := &openapi.Config{
		AccessKeyId:     &accessKeyID,
		AccessKeySecret: &accessKeySecret,
		Endpoint:        tea.String("domain.aliyuncs.com"),
	}

	client, err := domainSvc.NewClient(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// mapDomainItem converts an Alibaba SDK domain list item to NormalizedDomain.
func mapDomainItem(d *domainSvc.QueryDomainListResponseBodyDataDomain) domain.NormalizedDomain {
	now := time.Now()
	nd := domain.NormalizedDomain{
		RegistrarIdentifier: "alibaba",
		DataSourceType:      "api",
		LastSyncAt:          &now,
	}

	if d.DomainName != nil {
		nd.DomainName = *d.DomainName
	}
	if d.ExpirationDate != nil && *d.ExpirationDate != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", *d.ExpirationDate); err == nil {
			nd.ExpirationDate = &t
		} else if t, err := time.Parse("2006-01-02", *d.ExpirationDate); err == nil {
			nd.ExpirationDate = &t
		}
	}
	if d.ExpirationDateLong != nil {
		t := time.UnixMilli(*d.ExpirationDateLong)
		nd.ExpirationDate = &t
	}

	// Default to active - QueryDomainList returns owned domains
	// Only mark as expired if ExpirationDate is in the past
	nd.Status = "active"
	if nd.ExpirationDate != nil && nd.ExpirationDate.Before(time.Now()) {
		nd.Status = "expired"
	}

	return nd
}

// mapDomainDetail converts a QueryDomainByDomainName response to NormalizedDomain.
func mapDomainDetail(body *domainSvc.QueryDomainByDomainNameResponseBody) domain.NormalizedDomain {
	now := time.Now()
	nd := domain.NormalizedDomain{
		RegistrarIdentifier: "alibaba",
		DataSourceType:      "api",
		LastSyncAt:          &now,
	}

	if body.DomainName != nil {
		nd.DomainName = *body.DomainName
	}
	if body.ExpirationDate != nil && *body.ExpirationDate != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", *body.ExpirationDate); err == nil {
			nd.ExpirationDate = &t
		}
	}
	if body.RegistrationDate != nil && *body.RegistrationDate != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", *body.RegistrationDate); err == nil {
			nd.CreationDate = &t
		}
	}
	nd.Status = "active"
	if nd.ExpirationDate != nil && nd.ExpirationDate.Before(time.Now()) {
		nd.Status = "expired"
	}

	if body.DnsList != nil && body.DnsList.Dns != nil {
		dnsList := make([]string, 0, len(body.DnsList.Dns))
		for _, dns := range body.DnsList.Dns {
			if dns != nil {
				dnsList = append(dnsList, *dns)
			}
		}
		nd.Nameservers = domain.JSON(dnsList)
	}

	return nd
}

// mapDomainStatus maps Alibaba's DomainStatus to DomainRadar status.
// Alibaba returns numeric strings: 1=急需续费, 2=急需赎回, 3=正常
// Or other values. We default to "active" for most cases.
func mapDomainStatus(status string) string {
	switch status {
	case "3", "正常": // 3=normal
		return "active"
	case "1", "急需续费": // 1=needs renewal urgently
		return "active" // still active, just needs renewal
	case "2", "急需赎回": // 2=needs redemption
		return "expired"
	case "4", "pendingDelete":
		return "expired"
	default:
		// Default to active — most domains from QueryDomainList are active
		return "active"
	}
}

// Compile-time check.
var _ adapter.RegistrarAdapter = (*Adapter)(nil)
