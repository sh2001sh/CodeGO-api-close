package app

import (
	"errors"
	"fmt"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
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
		var sub commerceschema.UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", reward.UserSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "subscription",
			OwnerType:   "user_subscription",
			OwnerID:     int64(sub.Id),
			QuotaUnit:   "quota",
		})
		if err != nil {
			return err
		}
		if err := bootstrapLuckySubscriptionLedgerTx(tx, account, &sub, reward.Id); err != nil {
			return err
		}
		if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID:      account.AccountID,
			Amount:         reward.FinalRewardQuota,
			IdempotencyKey: fmt.Sprintf("lucky-draw:%s:%d", drawDateForRewardTx(tx, reward.DrawId), reward.UserSubscriptionId),
			ReasonCode:     "subscription_lucky_draw",
			ReferenceType:  "subscription_lucky_reward",
			ReferenceID:    fmt.Sprintf("%d", reward.Id),
			OperatorType:   "commerce",
			OperatorID:     fmt.Sprintf("%d", reward.DrawId),
		}); err != nil {
			return err
		}
		sub.AmountTotal += reward.FinalRewardQuota
		if sub.PeriodAmount > 0 {
			sub.PeriodAmount += reward.FinalRewardQuota
		}
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		reward.CreditStatus = commerceschema.SubscriptionLuckyRewardCreditCredited
		reward.CreditError = ""
		reward.CreditedAt = platformruntime.GetTimestamp()
		return tx.Save(&reward).Error
	})
}

func drawDateForRewardTx(tx *gorm.DB, drawID int) string {
	var draw commerceschema.SubscriptionLuckyDraw
	if tx != nil && tx.Where("id = ?", drawID).First(&draw).Error == nil {
		return draw.DrawDate
	}
	return "unknown"
}

func bootstrapLuckySubscriptionLedgerTx(tx *gorm.DB, account *billingschema.BillingAccount, sub *commerceschema.UserSubscription, rewardID int) error {
	if tx == nil || account == nil || sub == nil {
		return errors.New("invalid lucky subscription ledger arguments")
	}
	var count int64
	if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	available := sub.AmountTotal - sub.AmountUsed
	if available <= 0 {
		return nil
	}
	_, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID:      account.AccountID,
		Amount:         available,
		IdempotencyKey: fmt.Sprintf("subscription-bootstrap:%d", sub.Id),
		ReasonCode:     "subscription_balance_bootstrap",
		ReferenceType:  "user_subscription",
		ReferenceID:    fmt.Sprintf("%d", sub.Id),
		OperatorType:   "subscription_projection",
		OperatorID:     fmt.Sprintf("lucky-reward:%d", rewardID),
	})
	return err
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
