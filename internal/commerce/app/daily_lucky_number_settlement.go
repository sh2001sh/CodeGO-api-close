package app

import (
	"errors"
	"fmt"
	"strings"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func settleDailyLuckyDraw(drawID int) error {
	if drawID <= 0 {
		return errors.New("invalid lucky draw id")
	}
	var draw commerceschema.SubscriptionLuckyDraw
	if err := platformdb.DB.Where("id = ?", drawID).First(&draw).Error; err != nil {
		return err
	}
	if draw.Status == commerceschema.SubscriptionLuckyDrawStatusCompleted {
		return nil
	}
	var rewards []commerceschema.SubscriptionLuckyReward
	if err := platformdb.DB.Where("draw_id = ? AND credit_status IN ?", drawID, []string{
		commerceschema.SubscriptionLuckyRewardCreditPending,
		commerceschema.SubscriptionLuckyRewardCreditFailed,
	}).Order("id asc").Find(&rewards).Error; err != nil {
		return err
	}
	for _, reward := range rewards {
		if err := settleDailyLuckyReward(reward.Id); err != nil {
			if markErr := markLuckyRewardFailed(reward.Id, err); markErr != nil {
				return fmt.Errorf("settle reward %d: %w; mark failed: %v", reward.Id, err, markErr)
			}
			_ = markLuckyDrawFailed(drawID, err)
			return err
		}
	}
	var pending int64
	if err := platformdb.DB.Model(&commerceschema.SubscriptionLuckyReward{}).
		Where("draw_id = ? AND credit_status IN ?", drawID, []string{
			commerceschema.SubscriptionLuckyRewardCreditPending,
			commerceschema.SubscriptionLuckyRewardCreditFailed,
		}).Count(&pending).Error; err != nil {
		return err
	}
	if pending > 0 {
		return errors.New("lucky draw still has unsettled rewards")
	}
	return platformdb.DB.Model(&commerceschema.SubscriptionLuckyDraw{}).Where("id = ?", drawID).Updates(map[string]any{
		"status":        commerceschema.SubscriptionLuckyDrawStatusCompleted,
		"completed_at":  platformruntime.GetTimestamp(),
		"error_message": "",
	}).Error
}

// settleDailyLuckyReward credits the reward into the user's ordinary wallet.
// Rewards are wallet balance, not subscription balance, so they survive the
// subscription period and stay usable after the plan expires.
func settleDailyLuckyReward(rewardID int) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var reward commerceschema.SubscriptionLuckyReward
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", rewardID).First(&reward).Error; err != nil {
			return err
		}
		if reward.CreditStatus == commerceschema.SubscriptionLuckyRewardCreditCredited {
			return nil
		}
		if reward.FinalRewardQuota <= 0 {
			reward.CreditStatus = commerceschema.SubscriptionLuckyRewardCreditCredited
			reward.CreditError = ""
			reward.CreditedAt = platformruntime.GetTimestamp()
			return tx.Save(&reward).Error
		}
		userID := reward.UserId
		if userID <= 0 {
			var sub commerceschema.UserSubscription
			if err := tx.Where("id = ?", reward.UserSubscriptionId).First(&sub).Error; err != nil {
				return err
			}
			userID = sub.UserId
		}
		if userID <= 0 {
			return fmt.Errorf("lucky reward %d has no owner user", reward.Id)
		}
		idempotencyKey := fmt.Sprintf("lucky-draw:%s:%d", drawDateForRewardTx(tx, reward.DrawId), reward.UserSubscriptionId)
		if err := billingapp.CreditWalletQuotaTx(tx, userID, int(reward.FinalRewardQuota), idempotencyKey, "subscription_lucky_draw"); err != nil {
			return err
		}
		if err := recordLuckyRewardLogTx(tx, userID, &reward); err != nil {
			return err
		}
		reward.CreditStatus = commerceschema.SubscriptionLuckyRewardCreditCredited
		reward.CreditError = ""
		reward.CreditedAt = platformruntime.GetTimestamp()
		return tx.Save(&reward).Error
	})
}

func recordLuckyRewardLogTx(tx *gorm.DB, userID int, reward *commerceschema.SubscriptionLuckyReward) error {
	if tx == nil || userID <= 0 || reward == nil {
		return errors.New("invalid lucky reward log params")
	}
	content := fmt.Sprintf(
		"每日幸运号中奖到账，钱包：额度，到账额度：%s，命中位数：%d，幸运号：%s，奖励记录ID：%d",
		logger.LogQuota(int(reward.FinalRewardQuota)),
		reward.MatchedDigits,
		reward.LuckyNumber,
		reward.Id,
	)
	return auditapp.RecordLogTx(tx, userID, auditschema.LogTypeTopup, content)
}

func drawDateForRewardTx(tx *gorm.DB, drawID int) string {
	var draw commerceschema.SubscriptionLuckyDraw
	if tx != nil && tx.Where("id = ?", drawID).First(&draw).Error == nil {
		return draw.DrawDate
	}
	return "unknown"
}

func markLuckyDrawFailed(drawID int, cause error) error {
	message := "unknown lucky draw settlement error"
	if cause != nil {
		message = strings.TrimSpace(cause.Error())
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return platformdb.DB.Model(&commerceschema.SubscriptionLuckyDraw{}).Where("id = ?", drawID).Updates(map[string]any{
		"status":        commerceschema.SubscriptionLuckyDrawStatusFailed,
		"error_message": message,
	}).Error
}

func markLuckyRewardFailed(rewardID int, cause error) error {
	message := "unknown lucky reward settlement error"
	if cause != nil {
		message = strings.TrimSpace(cause.Error())
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return platformdb.DB.Model(&commerceschema.SubscriptionLuckyReward{}).Where("id = ?", rewardID).Updates(map[string]any{
		"credit_status": commerceschema.SubscriptionLuckyRewardCreditFailed,
		"credit_error":  message,
	}).Error
}

// RetryDailyLuckyDraw settles an existing draw without generating a new number.
func RetryDailyLuckyDraw(drawID int) error {
	return settleDailyLuckyDraw(drawID)
}
