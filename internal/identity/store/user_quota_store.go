package store

import (
	"errors"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"

	"github.com/bytedance/gopkg/util/gopool"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// IncreaseUserQuota increments a user's wallet quota and mirrors the change into cache.
func IncreaseUserQuota(userID int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	gopool.Go(func() {
		if err := cacheAdjustUserQuota(userID, int64(quota)); err != nil {
			platformobservability.SysLog("failed to increase user quota: " + err.Error())
		}
	})
	return platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota + ?", quota)).Error
}

// DecreaseUserQuota decrements a user's wallet quota and mirrors the change into cache.
func DecreaseUserQuota(userID int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	gopool.Go(func() {
		if err := cacheAdjustUserQuota(userID, -int64(quota)); err != nil {
			platformobservability.SysLog("failed to decrease user quota: " + err.Error())
		}
	})
	return platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota - ?", quota)).Error
}

// IncreaseUserClaudeQuota increments a user's Claude wallet quota and mirrors the change into cache.
func IncreaseUserClaudeQuota(userID int, quota int) error {
	if quota < 0 {
		return errors.New("claude quota 不能为负数！")
	}
	gopool.Go(func() {
		if err := cacheAdjustUserClaudeQuota(userID, int64(quota)); err != nil {
			platformobservability.SysLog("failed to increase user claude quota: " + err.Error())
		}
	})
	return platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Update("claude_quota", gorm.Expr("claude_quota + ?", quota)).Error
}

// DecreaseUserClaudeQuota decrements a user's Claude wallet quota and mirrors the change into cache.
func DecreaseUserClaudeQuota(userID int, quota int) error {
	if quota < 0 {
		return errors.New("claude quota 不能为负数！")
	}
	gopool.Go(func() {
		if err := cacheAdjustUserClaudeQuota(userID, -int64(quota)); err != nil {
			platformobservability.SysLog("failed to decrease user claude quota: " + err.Error())
		}
	})
	return platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Update("claude_quota", gorm.Expr("claude_quota - ?", quota)).Error
}

// UpdateUserUsedQuotaAndRequestCount increments usage counters after a successful billed request.
func UpdateUserUsedQuotaAndRequestCount(userID int, quota int) {
	if quota <= 0 {
		return
	}
	if err := platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Updates(map[string]any{
		"used_quota":    gorm.Expr("used_quota + ?", quota),
		"request_count": gorm.Expr("request_count + ?", 1),
	}).Error; err != nil {
		platformobservability.SysLog("failed to update user used quota and request count: " + err.Error())
	}
}

// UpdateUserUsedQuota adjusts accumulated usage without changing request count.
func UpdateUserUsedQuota(userID int, quota int) {
	if quota == 0 {
		return
	}
	if err := platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).
		Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
		platformobservability.SysLog("failed to update user used quota: " + err.Error())
	}
}

func cacheAdjustUserQuota(userID int, delta int64) error {
	return platformcache.AdjustUserQuotaCache(userID, delta)
}

func cacheAdjustUserClaudeQuota(userID int, delta int64) error {
	return platformcache.AdjustUserClaudeQuotaCache(userID, delta)
}
