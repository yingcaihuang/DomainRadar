package tencent

import (
	"testing"

	"github.com/stretchr/testify/require"
	domainSvc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/domain/v20180808"
)

func TestMapDomainItem_OptionalFields(t *testing.T) {
	disabled, enabled := uint64(0), uint64(1)
	for _, value := range []*uint64{nil, &disabled, &enabled} {
		d := mapDomainItem(&domainSvc.DomainList{AutoRenew: value})
		require.Nil(t, d.PrivacyProtection)
		require.Nil(t, d.LockStatus)
		require.Nil(t, d.Nameservers)
		require.Nil(t, d.ExpirationDate)
		require.Empty(t, d.Status)
		if value == nil {
			require.Nil(t, d.AutoRenew)
		} else {
			require.NotNil(t, d.AutoRenew)
			require.Equal(t, *value == 1, *d.AutoRenew)
		}
	}
}
