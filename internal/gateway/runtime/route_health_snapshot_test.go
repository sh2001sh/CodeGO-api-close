package runtime

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestRouteHealthSnapshotPreservesUserIsolationAndSharedMetrics(t *testing.T) {
	require.NoError(t, resetChannelHealthForTest())
	t.Cleanup(func() { require.NoError(t, resetChannelHealthForTest()) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(string(constant.ContextKeyUserId), 42)
	model, kind := "snapshot-model", RequestTypeChatShortStream
	domain := "snapshot-provider"
	RecordChannelSuccess(701, model, 350*time.Millisecond, kind)
	RecordChannelCredentialFailure(701)
	RecordUserChannelGatewayFailureForRequest(ctx, 701, model, "failed-1", 502, kind)
	RecordUserChannelGatewayFailureForRequest(ctx, 701, model, "failed-2", 502, kind)
	RecordUserFaultDomainFailure(ctx, domain, model, "domain-1", time.Minute, kind)
	snapshot, err := LoadRouteHealthSnapshot(ctx, []int{701, 702, 701}, []string{domain, domain, ""}, model, kind)
	require.NoError(t, err)
	require.Len(t, snapshot.Channels, 1)
	require.Equal(t, ChannelHealthCooling, snapshot.Channels[701].State)
	require.Greater(t, snapshot.Channels[701].TTFTP95Milliseconds, float64(0))
	expectedDomain, found := GetUserFaultDomainHealth(ctx, domain, model, kind)
	require.True(t, found)
	require.Equal(t, expectedDomain, snapshot.Domains[domain])
	require.Equal(t, ChannelHealthCooling, snapshot.Credentials[701].State)
	ctx.Set(string(constant.ContextKeyUserId), 43)
	other, err := LoadRouteHealthSnapshot(ctx, []int{701}, []string{domain}, model, kind)
	require.NoError(t, err)
	require.Empty(t, other.Channels[701].State, "one user's cooling must not affect another")
	require.Equal(t, snapshot.Channels[701].TTFTP95Milliseconds, other.Channels[701].TTFTP95Milliseconds)
	require.Empty(t, other.Domains)
	other, err = LoadRouteHealthSnapshot(ctx, []int{701}, nil, model, RequestTypeChatLongStream)
	require.NoError(t, err)
	require.Empty(t, other.Channels, "health stays isolated by request type")
}
