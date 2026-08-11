package http

import (
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const zeroHourConcurrentRequests = 5

func specialMultiplierConcurrentRequests(c *gin.Context) int64 {
	if httpctx.GetContextKeyBool(c, constant.ContextKeyMonthlyPassActive) {
		// Lite/Standard cards run at one request; Pro/Ultra at two. The duration
		// encodes the issued monthly tier without exposing subscription details to clients.
		return commerceapp.MonthlyPassConcurrentRequests(c.GetInt("id"))
	}
	return zeroHourConcurrentRequests
}

func acquireZeroHourSlot(c *gin.Context) (func(), error) {
	if !httpctx.GetContextKeyBool(c, constant.ContextKeyZeroHourActive) && !httpctx.GetContextKeyBool(c, constant.ContextKeyMonthlyPassActive) {
		return func() {}, nil
	}
	if !platformcache.RedisReady() {
		return nil, errors.New("倍率卡分组并发控制暂不可用，请稍后重试")
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		return nil, errors.New("倍率卡分组用户状态无效")
	}
	prefix := "zero-hour"
	if httpctx.GetContextKeyBool(c, constant.ContextKeyMonthlyPassActive) {
		prefix = "monthly-pass"
	}
	key := fmt.Sprintf("codego:%s:concurrency:%d", prefix, userID)
	ctx := c.Request.Context()
	count, err := platformcache.RDB.Incr(ctx, key).Result()
	if err != nil {
		return nil, errors.New("倍率卡分组并发控制暂不可用，请稍后重试")
	}
	if count == 1 {
		_ = platformcache.RDB.Expire(ctx, key, 2*time.Hour).Err()
	}
	limit := specialMultiplierConcurrentRequests(c)
	if count > limit {
		_ = platformcache.RDB.Decr(ctx, key).Err()
		return nil, fmt.Errorf("倍率卡分组单用户并发已达 %d，请等待当前请求完成", limit)
	}
	return func() {
		_ = platformcache.RDB.Decr(ctx, key).Err()
	}, nil
}
