package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayUserConcurrencyRejectsBeforeBodyAndBilling(t *testing.T) {
	addr := os.Getenv("USER_CONCURRENCY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated Redis")
	}
	oldLimits, oldClient, oldEnabled := platformconfig.UserMaxConcurrentRequests, platformcache.RDB, platformcache.RedisEnabled
	client := redis.NewClient(&redis.Options{Addr: addr})
	platformconfig.UserMaxConcurrentRequests = map[int]int{900003: 1}
	platformcache.RDB, platformcache.RedisEnabled = client, true
	t.Cleanup(func() {
		_ = client.Close()
		platformconfig.UserMaxConcurrentRequests, platformcache.RDB, platformcache.RedisEnabled = oldLimits, oldClient, oldEnabled
	})
	_, release, admission := relaycommon.TryBeginUserRequest(context.Background(), 900003)
	require.Equal(t, relaycommon.ChannelConcurrencyAdmitted, admission)
	defer release()
	for _, tokenID := range []int{1, 2} {
		body := &readCountingBody{}
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", body)
		ctx.Set("id", 900003)
		ctx.Set("token_id", tokenID)
		httpctx.SetContextKey(ctx, constant.ContextKeyBypassModelRequestLimits, true)
		relayRequest(ctx, types.RelayFormatOpenAIResponses)
		require.Equal(t, http.StatusTooManyRequests, recorder.Code)
		require.Equal(t, "2", recorder.Header().Get("Retry-After"))
		require.Zero(t, body.reads)
	}
}
