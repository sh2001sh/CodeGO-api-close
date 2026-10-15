package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"gorm.io/gorm"
)

const dailyLuckyNumberTaskInterval = time.Minute

var dailyLuckyNumberTaskOnce sync.Once

// StartDailyLuckyNumberTask starts the durable daily draw loop for ledger-worker.
// Web processes never call this function, so they cannot create official draws.
func StartDailyLuckyNumberTask() {
	dailyLuckyNumberTaskOnce.Do(func() {
		if !platformconfig.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ctx := context.Background()
			logger.LogInfo(ctx, fmt.Sprintf("daily lucky number task started: tick=%s", dailyLuckyNumberTaskInterval))
			runDailyLuckyNumberOnce(time.Now())
			ticker := time.NewTicker(dailyLuckyNumberTaskInterval)
			defer ticker.Stop()
			for range ticker.C {
				runDailyLuckyNumberOnce(time.Now())
			}
		})
	})
}

func runDailyLuckyNumberOnce(now time.Time) {
	if !subscriptionLuckyNumberTableReady() {
		return
	}

	settleUnfinishedLuckyDraws()
	setting := luckysettings.Get()
	if !setting.Enabled {
		return
	}
	location, err := setting.Location()
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("daily lucky number timezone is invalid: %v", err))
		return
	}
	localNow := now.In(location)
	for offset := 1; offset >= 0; offset-- {
		date := localNow.AddDate(0, 0, -offset)
		drawAt := time.Date(date.Year(), date.Month(), date.Day(), setting.DrawHour, setting.DrawMinute, 0, 0, location)
		if localNow.Before(drawAt) {
			continue
		}
		drawDate := date.Format(luckyDrawDateLayout)
		draw, createErr := createDailyLuckyDraw(drawDate, drawAt, setting)
		if createErr != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("daily lucky number draw %s creation failed: %v", drawDate, createErr))
			continue
		}
		if draw != nil && draw.Status != commerceschema.SubscriptionLuckyDrawStatusCompleted {
			if settleErr := settleDailyLuckyDraw(draw.Id); settleErr != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("daily lucky number draw %s settlement failed: %v", drawDate, settleErr))
			}
		}
	}
}

func settleUnfinishedLuckyDraws() {
	var draws []commerceschema.SubscriptionLuckyDraw
	err := platformdb.DB.Where("status IN ?", []string{
		commerceschema.SubscriptionLuckyDrawStatusPending,
		commerceschema.SubscriptionLuckyDrawStatusSettling,
		commerceschema.SubscriptionLuckyDrawStatusFailed,
	}).Order("id asc").Limit(20).Find(&draws).Error
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("daily lucky number unfinished draw query failed: %v", err))
		return
	}
	for _, draw := range draws {
		if err := settleDailyLuckyDraw(draw.Id); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("daily lucky number retry failed: draw_id=%d error=%v", draw.Id, err))
		}
	}
}

func drawExistsByDate(tx *gorm.DB, drawDate string) bool {
	if tx == nil || drawDate == "" {
		return false
	}
	var count int64
	return tx.Model(&commerceschema.SubscriptionLuckyDraw{}).Where("draw_date = ?", drawDate).Count(&count).Error == nil && count > 0
}
