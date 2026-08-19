package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
)

var ErrTokenQuotaInsufficient = errors.New("token quota is insufficient")

// AdjustTokenQuota applies a signed quota delta and mirrors the committed
// remaining quota into Redis.
func AdjustTokenQuota(tokenID int, tokenKey string, delta int) error {
	if delta == 0 {
		return nil
	}
	tokenKey = strings.TrimSpace(tokenKey)
	if err := AdjustTokenQuotaTx(platformdb.DB, tokenID, delta); err != nil {
		return err
	}
	ProjectTokenQuotaCache(tokenKey, -int64(delta))
	return nil
}

// AdjustTokenQuotaTx atomically updates persisted token quota inside the
// caller's transaction. Cache projection must happen after commit.
func AdjustTokenQuotaTx(tx *gorm.DB, tokenID int, delta int) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if tokenID <= 0 || delta == 0 {
		return nil
	}
	if delta < 0 {
		return increaseTokenQuotaTx(tx, tokenID, -delta)
	}
	return decreaseTokenQuotaTx(tx, tokenID, delta)
}

func increaseTokenQuotaTx(tx *gorm.DB, tokenID int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	return tx.Model(&identityschema.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"remain_quota":  gorm.Expr("remain_quota + ?", quota),
		"used_quota":    gorm.Expr("used_quota - ?", quota),
		"accessed_time": platformruntime.GetTimestamp(),
	}).Error
}

func decreaseTokenQuotaTx(tx *gorm.DB, tokenID int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	result := tx.Model(&identityschema.Token{}).
		Where("id = ? AND remain_quota >= ?", tokenID, quota).
		Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": platformruntime.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrTokenQuotaInsufficient
	}
	return nil
}

// ProjectTokenQuotaCache mirrors a committed token quota delta to Redis.
func ProjectTokenQuotaCache(tokenKey string, delta int64) {
	tokenKey = strings.TrimSpace(tokenKey)
	if platformcache.RedisEnabled {
		gopool.Go(func() {
			if err := cacheAdjustTokenQuota(tokenKey, delta); err != nil {
				platformobservability.SysLog("failed to project token quota cache: " + err.Error())
			}
		})
	}
}

func cacheAdjustTokenQuota(key string, delta int64) error {
	if !platformcache.RedisReady() {
		return nil
	}
	return platformcache.RedisHIncrBy(fmt.Sprintf("token:%s", platformsecurity.GenerateHMAC(key)), constant.TokenFiledRemainQuota, delta)
}
