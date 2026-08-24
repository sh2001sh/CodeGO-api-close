package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const multiplierCardConcurrentRequests = 10

func acquireMultiplierCardSlot(c *gin.Context) (func(), error) {
	keyPrefix, cardName, enabled := multiplierCardSlotPolicy(c)
	if !enabled {
		return func() {}, nil
	}
	if !platformcache.RedisReady() {
		return nil, errors.New("倍率卡分组并发控制暂不可用，请稍后重试")
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		return nil, errors.New("倍率卡分组用户状态无效")
	}
	key := fmt.Sprintf("%s:%d", keyPrefix, userID)
	ctx := c.Request.Context()
	count, err := platformcache.RDB.Incr(ctx, key).Result()
	if err != nil {
		return nil, errors.New("倍率卡分组并发控制暂不可用，请稍后重试")
	}
	if count == 1 {
		_ = platformcache.RDB.Expire(ctx, key, 2*time.Hour).Err()
	}
	limit := int64(multiplierCardConcurrentRequests)
	if count > limit {
		_ = platformcache.RDB.Decr(ctx, key).Err()
		return nil, fmt.Errorf("%s单用户并发已达 %d，请等待当前请求完成", cardName, limit)
	}
	return func() {
		_ = platformcache.RDB.Decr(context.Background(), key).Err()
	}, nil
}

func multiplierCardSlotPolicy(c *gin.Context) (keyPrefix string, cardName string, enabled bool) {
	if httpctx.GetContextKeyBool(c, constant.ContextKeyZeroHourActive) {
		return "codego:zero-hour:concurrency", "0 倍率卡", true
	}
	if httpctx.GetContextKeyBool(c, constant.ContextKeyMonthlyPassActive) {
		return "codego:monthly-pass:concurrency", "0.1 倍率卡", true
	}
	return "", "", false
}
