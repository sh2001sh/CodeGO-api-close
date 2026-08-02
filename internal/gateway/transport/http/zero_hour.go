package http

import (
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const zeroHourConcurrentRequests = 5

func acquireZeroHourSlot(c *gin.Context) (func(), error) {
	if !httpctx.GetContextKeyBool(c, constant.ContextKeyZeroHourActive) {
		return func() {}, nil
	}
	if !platformcache.RedisReady() {
		return nil, errors.New("0 倍率分组并发控制暂不可用，请稍后重试")
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		return nil, errors.New("0 倍率分组用户状态无效")
	}
	key := fmt.Sprintf("codego:zero-hour:concurrency:%d", userID)
	ctx := c.Request.Context()
	count, err := platformcache.RDB.Incr(ctx, key).Result()
	if err != nil {
		return nil, errors.New("0 倍率分组并发控制暂不可用，请稍后重试")
	}
	if count == 1 {
		_ = platformcache.RDB.Expire(ctx, key, 2*time.Hour).Err()
	}
	if count > zeroHourConcurrentRequests {
		_ = platformcache.RDB.Decr(ctx, key).Err()
		return nil, fmt.Errorf("0 倍率分组单用户并发已达 %d，请等待当前请求完成", zeroHourConcurrentRequests)
	}
	return func() {
		_ = platformcache.RDB.Decr(ctx, key).Err()
	}, nil
}
