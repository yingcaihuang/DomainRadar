package tencent

import (
	"context"
	"fmt"
	"time"

	"domainradar/internal/adapter"
	"domainradar/internal/domain"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	domainSvc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/domain/v20180808"
)

// TencentAdapter implements RegistrarAdapter for Tencent Cloud Domain API using the official SDK.
type TencentAdapter struct{}

// New creates a new TencentAdapter.
func New() *TencentAdapter {
	return &TencentAdapter{}
}

// RegistrarType returns "tencent".
func (a *TencentAdapter) RegistrarType() string {
	return "tencent"
}

// TestConnection validates credentials by calling DescribeDomainNameList with Limit=1.
func (a *TencentAdapter) TestConnection(ctx context.Context, credential *adapter.RegistrarCredential) error {
	client, err := a.newClient(credential)
	if err != nil {
		return fmt.Errorf("tencent: failed to create client: %w", err)
	}

	request := domainSvc.NewDescribeDomainNameListRequest()
	limit := uint64(1)
	offset := uint64(0)
	request.Limit = &limit
	request.Offset = &offset

	_, err = client.DescribeDomainNameList(request)
	if err != nil {
		return fmt.Errorf("tencent: %v", err)
	}

	return nil
}

// ListDomains retrieves all domains from the Tencent Cloud account.
func (a *TencentAdapter) ListDomains(ctx context.Context, credential *adapter.RegistrarCredential) ([]domain.NormalizedDomain, error) {
	client, err := a.newClient(credential)
	if err != nil {
		return nil, fmt.Errorf("tencent: failed to create client: %w", err)
	}

	var allDomains []domain.NormalizedDomain
	var offset uint64 = 0
	var limit uint64 = 100

	for {
		request := domainSvc.NewDescribeDomainNameListRequest()
		request.Limit = &limit
		request.Offset = &offset

		response, err := client.DescribeDomainNameList(request)
		if err != nil {
			return nil, fmt.Errorf("tencent: %v", err)
		}

		if response.Response == nil || response.Response.DomainSet == nil {
			break
		}

		for _, d := range response.Response.DomainSet {
			nd := mapDomainItem(d)
			allDomains = append(allDomains, nd)
		}

		var totalCount uint64 = 0
		if response.Response.TotalCount != nil {
			totalCount = *response.Response.TotalCount
		}

		if uint64(len(allDomains)) >= totalCount || uint64(len(response.Response.DomainSet)) < limit {
			break
		}
		offset += limit
	}

	return allDomains, nil
}

// GetDomainDetail retrieves detailed info for a specific domain.
func (a *TencentAdapter) GetDomainDetail(ctx context.Context, credential *adapter.RegistrarCredential, domainName string) (*domain.NormalizedDomain, error) {
	client, err := a.newClient(credential)
	if err != nil {
		return nil, fmt.Errorf("tencent: failed to create client: %w", err)
	}

	request := domainSvc.NewDescribeDomainBaseInfoRequest()
	request.Domain = &domainName

	response, err := client.DescribeDomainBaseInfo(request)
	if err != nil {
		return nil, fmt.Errorf("tencent: %v", err)
	}

	if response.Response == nil || response.Response.DomainInfo == nil {
		return nil, fmt.Errorf("tencent: empty response for domain %s", domainName)
	}

	nd := mapDomainBaseInfo(response.Response.DomainInfo)
	return &nd, nil
}

// newClient creates a Tencent Cloud domain API client from credentials.
func (a *TencentAdapter) newClient(credential *adapter.RegistrarCredential) (*domainSvc.Client, error) {
	secretID := credential.AccessKeyID
	secretKey := credential.SecretAccessKey

	if secretID == "" || secretKey == "" {
		return nil, fmt.Errorf("missing SecretId or SecretKey")
	}

	cred := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "domain.tencentcloudapi.com"

	client, err := domainSvc.NewClient(cred, "", cpf)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// mapDomainItem converts a Tencent SDK DomainList item to NormalizedDomain.
func mapDomainItem(d *domainSvc.DomainList) domain.NormalizedDomain {
	now := time.Now()
	nd := domain.NormalizedDomain{
		RegistrarIdentifier: "tencent",
		DataSourceType:      "api",
		LastSyncAt:          &now,
	}

	if d.DomainName != nil {
		nd.DomainName = *d.DomainName
	}
	if d.AutoRenew != nil {
		nd.AutoRenew = *d.AutoRenew == 1
	}
	if d.CreationDate != nil && *d.CreationDate != "" {
		if t, err := time.Parse("2006-01-02", *d.CreationDate); err == nil {
			nd.CreationDate = &t
		}
	}
	if d.ExpirationDate != nil && *d.ExpirationDate != "" {
		if t, err := time.Parse("2006-01-02", *d.ExpirationDate); err == nil {
			nd.ExpirationDate = &t
		}
	}
	if d.BuyStatus != nil {
		nd.Status = mapBuyStatus(*d.BuyStatus)
	} else {
		nd.Status = "active"
	}

	return nd
}

// mapDomainBaseInfo converts a DescribeDomainBaseInfo response to NormalizedDomain.
func mapDomainBaseInfo(info *domainSvc.DomainBaseInfo) domain.NormalizedDomain {
	now := time.Now()
	nd := domain.NormalizedDomain{
		RegistrarIdentifier: "tencent",
		DataSourceType:      "api",
		LastSyncAt:          &now,
	}

	if info.DomainName != nil {
		nd.DomainName = *info.DomainName
	}
	if info.CreationDate != nil && *info.CreationDate != "" {
		if t, err := time.Parse("2006-01-02", *info.CreationDate); err == nil {
			nd.CreationDate = &t
		}
	}
	if info.ExpirationDate != nil && *info.ExpirationDate != "" {
		if t, err := time.Parse("2006-01-02", *info.ExpirationDate); err == nil {
			nd.ExpirationDate = &t
		}
	}

	nd.Status = "active"

	return nd
}

// mapBuyStatus maps Tencent's BuyStatus string to DomainRadar status.
func mapBuyStatus(status string) string {
	switch status {
	case "ok", "Normal", "":
		return "active"
	case "AboutToExpire":
		return "active" // still active, just expiring soon
	case "Expired":
		return "expired"
	case "RedemptionPending":
		return "expired"
	default:
		return "active"
	}
}

// Compile-time check that TencentAdapter implements RegistrarAdapter.
var _ adapter.RegistrarAdapter = (*TencentAdapter)(nil)
