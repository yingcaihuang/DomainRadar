package alibaba

import (
	"testing"

	domainSvc "github.com/alibabacloud-go/domain-20180129/v4/client"
	"github.com/stretchr/testify/require"
)

func TestMapDomainItem_OptionalFields(t *testing.T) {
	d := mapDomainItem(&domainSvc.QueryDomainListResponseBodyDataDomain{})
	require.Nil(t, d.AutoRenew)
	require.Nil(t, d.PrivacyProtection)
	require.Nil(t, d.LockStatus)
	require.Nil(t, d.Nameservers)
	require.Nil(t, d.ExpirationDate)
	require.Empty(t, d.Status)
}
