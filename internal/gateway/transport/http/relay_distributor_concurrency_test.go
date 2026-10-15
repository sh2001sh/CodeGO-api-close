package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	execution "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	runtime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	schema "github.com/sh2001sh/new-api/internal/gateway/schema"
	cache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestFirstDistributorAttemptEnforcesAndReleasesConcurrency(t *testing.T) {
	old := cache.RedisEnabled
	cache.RedisEnabled = false
	t.Cleanup(func() { cache.RedisEnabled = old })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	original := &schema.Channel{Id: 990071, Type: constant.ChannelTypeOpenAI, Key: "test", MarketplaceMaxConcurrency: 2, MarketplaceUserMaxConcurrency: 1}
	require.Nil(t, execution.SetupContextForSelectedChannel(c, original, "test"))
	selected := selectedDistributorChannel(c)
	require.Equal(t, 2, selected.MarketplaceMaxConcurrency)
	require.Equal(t, 1, selected.MarketplaceUserMaxConcurrency)
	release, status := runtime.TryBeginChannelRequestForUser(selected.Id, 1, selected.MarketplaceMaxConcurrency, selected.MarketplaceUserMaxConcurrency)
	require.Equal(t, runtime.ChannelConcurrencyAdmitted, status)
	defer release()
	_, status = runtime.TryBeginChannelRequestForUser(selected.Id, 1, selected.MarketplaceMaxConcurrency, selected.MarketplaceUserMaxConcurrency)
	require.Equal(t, runtime.ChannelConcurrencyCapacityReached, status)
	releaseOther, status := runtime.TryBeginChannelRequestForUser(selected.Id, 2, selected.MarketplaceMaxConcurrency, selected.MarketplaceUserMaxConcurrency)
	require.Equal(t, runtime.ChannelConcurrencyAdmitted, status)
	defer releaseOther()
	_, status = runtime.TryBeginChannelRequestForUser(selected.Id, 3, selected.MarketplaceMaxConcurrency, selected.MarketplaceUserMaxConcurrency)
	require.Equal(t, runtime.ChannelConcurrencyCapacityReached, status)
	release()
	releaseOther()
	require.Zero(t, runtime.ActiveChannelRequests(selected.Id))
	original.MarketplaceMaxConcurrency = 0
	original.MarketplaceUserMaxConcurrency = 0
	require.Nil(t, execution.SetupContextForSelectedChannel(c, original, "test"))
	require.Zero(t, selectedDistributorChannel(c).MarketplaceMaxConcurrency)
	require.Zero(t, selectedDistributorChannel(c).MarketplaceUserMaxConcurrency)
}
