package app

import (
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"strconv"
	"strings"
	"time"
)

func currentResetOpportunityMonth() string {
	return time.Now().In(time.Local).Format("2006-01")
}

func buildSubscriptionResetOpportunitySummary(account *commerceschema.SubscriptionResetOpportunityAccount) commerceschema.SubscriptionResetOpportunitySummary {
	summary := commerceschema.SubscriptionResetOpportunitySummary{
		CurrentMonth: currentResetOpportunityMonth(),
	}
	if account == nil {
		return summary
	}
	summary.AvailableCount = account.AvailableTotal
	summary.EarnedTotal = account.EarnedTotal
	summary.UsedTotal = account.UsedTotal
	summary.LastUsedMonth = strings.TrimSpace(account.LastUsedMonth)
	summary.UsedThisMonth = summary.LastUsedMonth != "" && summary.LastUsedMonth == summary.CurrentMonth
	return summary
}

func getOrCreateSubscriptionResetOpportunityAccountTx(tx *gorm.DB, userID int) (*commerceschema.SubscriptionResetOpportunityAccount, error) {
	if tx == nil {
		tx = platformdb.DB
	}
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}

	account := &commerceschema.SubscriptionResetOpportunityAccount{}
	err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(account).Error
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	account = &commerceschema.SubscriptionResetOpportunityAccount{UserId: userID}
	if err := tx.Create(account).Error; err != nil {
		return nil, err
	}
	return account, nil
}

func countSuccessfulPaidMonthCardPurchasesTx(tx *gorm.DB, userID int, excludedSubscriptionTradeNo string) (int64, error) {
	if tx == nil {
		tx = platformdb.DB
	}
	if userID <= 0 {
		return 0, nil
	}
	var monthCardCount int64
	monthCardQuery := tx.Model(&commerceschema.SubscriptionOrder{}).
		Joins("JOIN subscription_plans ON subscription_plans.id = subscription_orders.plan_id").
		Where("subscription_orders.user_id = ? AND subscription_orders.status = ? AND subscription_orders.money > 0", userID, constant.TopUpStatusSuccess).
		Where("subscription_plans.plan_type = ?", commerceschema.SubscriptionPlanTypeMonthly)
	if excludedSubscriptionTradeNo = strings.TrimSpace(excludedSubscriptionTradeNo); excludedSubscriptionTradeNo != "" {
		monthCardQuery = monthCardQuery.Where("subscription_orders.trade_no <> ?", excludedSubscriptionTradeNo)
	}
	if err := monthCardQuery.Count(&monthCardCount).Error; err != nil {
		return 0, err
	}
	return monthCardCount, nil
}

// GetUserSubscriptionResetOpportunity returns the current reset-opportunity summary.
func GetUserSubscriptionResetOpportunity(userID int) (*commerceschema.SubscriptionResetOpportunitySummary, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}

	account := &commerceschema.SubscriptionResetOpportunityAccount{}
	err := platformdb.DB.Where("user_id = ?", userID).First(account).Error
	if err == nil {
		summary := buildSubscriptionResetOpportunitySummary(account)
		return &summary, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		summary := buildSubscriptionResetOpportunitySummary(nil)
		return &summary, nil
	}
	return nil, err
}

// AwardReferralSubscriptionResetOpportunityTx awards a reset opportunity for inviter when invitee first buys a month card.
func AwardReferralSubscriptionResetOpportunityTx(tx *gorm.DB, inviteeID int, purchaseType string, orderSourceType string, orderSourceID string) error {
	if tx == nil {
		tx = platformdb.DB
	}
	if inviteeID <= 0 || strings.TrimSpace(purchaseType) != commercedomain.ReferralPurchaseTypeMonthCard {
		return nil
	}

	inviterID, err := referralInviterIDTx(tx, inviteeID)
	if err != nil || inviterID <= 0 {
		return err
	}
	previousMonthCardCount, err := countSuccessfulPaidMonthCardPurchasesTx(tx, inviteeID, orderSourceID)
	if err != nil {
		return err
	}
	if previousMonthCardCount > 0 {
		return nil
	}

	eventKey := fmt.Sprintf("referral-reset-opportunity:%d", inviteeID)
	existing := &commerceschema.SubscriptionResetOpportunityLedger{}
	if err := tx.Where("event_key = ?", eventKey).First(existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	account, err := getOrCreateSubscriptionResetOpportunityAccountTx(tx, inviterID)
	if err != nil {
		return err
	}
	account.EarnedTotal++
	account.AvailableTotal++
	if err := tx.Save(account).Error; err != nil {
		return err
	}

	ledger := &commerceschema.SubscriptionResetOpportunityLedger{
		UserId:        inviterID,
		RelatedUserId: inviteeID,
		ChangeType:    commerceschema.SubscriptionResetOpportunityChangeEarn,
		Delta:         1,
		BalanceAfter:  account.AvailableTotal,
		SourceType:    strings.TrimSpace(orderSourceType),
		SourceRef:     strings.TrimSpace(orderSourceID),
		EventKey:      eventKey,
		Note:          "邀请新用户首购月卡赠送额度重置机会",
	}
	return tx.Create(ledger).Error
}

func referralInviterIDTx(tx *gorm.DB, inviteeID int) (int, error) {
	user := &identityschema.User{}
	if err := tx.Select("inviter_id").Where("id = ?", inviteeID).First(user).Error; err != nil {
		return 0, err
	}
	return user.InviterId, nil
}

// UseUserSubscriptionResetOpportunity clears the preferred active subscription usage once per month.
func UseUserSubscriptionResetOpportunity(userID int) (*commerceschema.SubscriptionResetOpportunityUseResult, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}

	now := commercestore.GetDBTimestamp()
	currentMonth := currentResetOpportunityMonth()
	setting, err := identitystore.LoadUserSetting(userID, false)
	if err != nil {
		setting = dto.UserSetting{}
	}
	explicitOrder := commercedomain.NormalizePositiveIntSlice(setting.SubscriptionOrderIds)
	result := &commerceschema.SubscriptionResetOpportunityUseResult{}

	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		account, err := getOrCreateSubscriptionResetOpportunityAccountTx(tx, userID)
		if err != nil {
			return err
		}
		if account.AvailableTotal <= 0 {
			return commerceschema.ErrSubscriptionResetOpportunityUnavailable
		}
		if strings.TrimSpace(account.LastUsedMonth) == currentMonth {
			return commerceschema.ErrSubscriptionResetOpportunityMonthlyUsed
		}

		var subs []commerceschema.UserSubscription
		if err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", now).
			Order("end_time asc, id asc").
			Find(&subs).Error; err != nil {
			return err
		}
		if len(subs) == 0 {
			return commerceschema.ErrSubscriptionResetOpportunityNoActiveSub
		}
		subs, err = orderActiveUserSubscriptionsWithExplicitOrderTx(tx, subs, explicitOrder)
		if err != nil {
			return err
		}
		if len(subs) == 0 {
			return commerceschema.ErrSubscriptionResetOpportunityNoActiveSub
		}

		sub := subs[0]
		result.UserSubscriptionId = sub.Id
		result.AmountUsedBefore = sub.AmountUsed
		result.PeriodUsedBefore = sub.PeriodUsed

		sub.AmountUsed = 0
		sub.PeriodUsed = 0
		sub.ModelUsage = ""
		if err := restoreSubscriptionLedgerBalanceAfterResetTx(tx, &sub, fmt.Sprintf("opportunity:%d:%s", userID, currentMonth)); err != nil {
			return err
		}
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}

		account.UsedTotal++
		account.AvailableTotal--
		account.LastUsedMonth = currentMonth
		if err := tx.Save(account).Error; err != nil {
			return err
		}

		ledger := &commerceschema.SubscriptionResetOpportunityLedger{
			UserId:        userID,
			RelatedUserId: sub.Id,
			ChangeType:    commerceschema.SubscriptionResetOpportunityChangeUse,
			Delta:         -1,
			BalanceAfter:  account.AvailableTotal,
			UsedMonth:     currentMonth,
			SourceType:    "user_subscription",
			SourceRef:     strconv.Itoa(sub.Id),
			EventKey:      fmt.Sprintf("use-reset-opportunity:%d:%s", userID, currentMonth),
			Note:          "使用额度重置机会清空当前订阅已用额度",
		}
		if err := tx.Create(ledger).Error; err != nil {
			return err
		}

		result.AmountUsedAfter = sub.AmountUsed
		result.PeriodUsedAfter = sub.PeriodUsed
		result.ClearedUsedAmount = result.AmountUsedBefore
		result.ResetOpportunity = buildSubscriptionResetOpportunitySummary(account)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
