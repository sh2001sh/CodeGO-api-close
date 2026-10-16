package app

import (
	"errors"
	"fmt"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errSubscriptionCandidateUnavailable = errors.New("subscription candidate unavailable")

// PreConsumeUserSubscription reserves quota through the first eligible active
// subscription. Candidate discovery is intentionally outside the write
// transaction so concurrent requests only lock the selected subscription.
func PreConsumeUserSubscription(requestID string, userID int, modelName string, amount int64) (*commercedomain.SubscriptionPreConsumeResult, error) {
	return preConsumeSubscriptionSource(requestID, userID, modelName, amount, false)
}

// PreConsumeCreditPack uses only immutable permanent-package balances. The
// caller must first verify that the selected channel accepts this source.
func PreConsumeCreditPack(requestID string, userID int, modelName string, amount int64) (*commercedomain.SubscriptionPreConsumeResult, error) {
	return preConsumeSubscriptionSource(requestID, userID, modelName, amount, true)
}

func preConsumeSubscriptionSource(requestID string, userID int, modelName string, amount int64, creditPack bool) (*commercedomain.SubscriptionPreConsumeResult, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}

	if result, found, err := findPreConsumedSubscription(requestID, userID, creditPack); err != nil || found {
		return result, err
	}

	now := commercestore.GetDBTimestamp()
	candidates, err := loadActiveSubscriptionSourceCandidates(userID, now, creditPack)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		plan, err := getSubscriptionPlanRecordTx(nil, candidate.PlanId)
		if err != nil {
			return nil, err
		}
		result, available, err := preConsumeSubscriptionCandidate(requestID, userID, strings.TrimSpace(modelName), amount, now, candidate.Id, plan)
		if err != nil {
			return nil, err
		}
		if available {
			if (result.CreditPackGrantedQuota > 0) != creditPack {
				return nil, errors.New("subscription funding source mismatch")
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("subscription quota insufficient, need=%d", amount)
}

func findPreConsumedSubscription(requestID string, userID int, creditPack bool) (*commercedomain.SubscriptionPreConsumeResult, bool, error) {
	var record commerceschema.SubscriptionPreConsumeRecord
	query := platformdb.DB.Where("request_id = ?", requestID).Limit(1).Find(&record)
	if query.Error != nil {
		return nil, false, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, false, nil
	}
	if record.UserId != userID || record.Status == "refunded" {
		return nil, true, errors.New("subscription pre-consume request unavailable")
	}
	result, err := subscriptionPreConsumeResultFromRecord(platformdb.DB, &record)
	if err == nil && (result.CreditPackGrantedQuota > 0) != creditPack {
		return nil, true, errors.New("subscription funding source mismatch")
	}
	return result, true, err
}

func loadActiveSubscriptionCandidates(userID int, now int64) ([]commerceschema.UserSubscription, error) {
	return loadActiveSubscriptionSourceCandidates(userID, now, false)
}

func loadActiveSubscriptionSourceCandidates(userID int, now int64, creditPack bool) ([]commerceschema.UserSubscription, error) {
	var subscriptions []commerceschema.UserSubscription
	query := platformdb.DB.Where("user_id = ? AND status = ?", userID, "active")
	if creditPack {
		query = query.Where("credit_pack_granted_quota > 0 AND end_time = 0")
	} else {
		query = query.Where("credit_pack_granted_quota = 0 AND end_time > ?", now)
	}
	if err := query.
		Order("end_time asc, id asc").
		Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	if len(subscriptions) == 0 {
		return nil, errors.New("no active subscription")
	}
	return orderActiveUserSubscriptions(userID, subscriptions)
}

func preConsumeSubscriptionCandidate(requestID string, userID int, modelName string, amount int64, now int64, subscriptionID int, plan *commerceschema.SubscriptionPlan) (*commercedomain.SubscriptionPreConsumeResult, bool, error) {
	result := &commercedomain.SubscriptionPreConsumeResult{}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing commerceschema.SubscriptionPreConsumeRecord
		query := tx.Where("request_id = ?", requestID).Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected > 0 {
			if existing.Status == "refunded" || existing.UserId != userID {
				return errors.New("subscription pre-consume already refunded")
			}
			var err error
			result, err = subscriptionPreConsumeResultFromRecord(tx, &existing)
			return err
		}

		subscription := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ? AND status = ? AND (end_time > ? OR (end_time = 0 AND credit_pack_granted_quota > 0))", subscriptionID, userID, "active", now).
			First(subscription).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errSubscriptionCandidateUnavailable
			}
			return err
		}
		if plan == nil || plan.Id != subscription.PlanId {
			var err error
			plan, err = getSubscriptionPlanRecordTx(tx, subscription.PlanId)
			if err != nil {
				return err
			}
		}
		plan = creditPackSnapshotPlan(plan, subscription)
		if err := maybeResetUserSubscriptionWithPlanTx(tx, subscription, plan, now); err != nil {
			return err
		}
		canPreConsume, lockedLedgerAccount, err := subscriptionCanPreConsume(tx, subscription, plan, modelName, amount)
		if err != nil {
			return err
		}
		if !canPreConsume {
			return errSubscriptionCandidateUnavailable
		}

		record := &commerceschema.SubscriptionPreConsumeRecord{
			RequestId: requestID, UserId: userID, UserSubscriptionId: subscription.Id,
			ModelName: modelName, PreConsumed: amount, Status: "consumed",
		}
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		reservation, err := reserveSubscriptionLedgerTx(tx, subscription, record, lockedLedgerAccount)
		if err != nil {
			return err
		}

		usedBefore := subscription.AmountUsed
		if err := applySubscriptionUsageDelta(plan, subscription, modelName, amount); err != nil {
			return err
		}
		if err := tx.Save(subscription).Error; err != nil {
			return err
		}
		result.UserSubscriptionId = subscription.Id
		result.PreConsumed = amount
		result.AmountTotal = subscription.AmountTotal
		result.AmountUsedBefore = usedBefore
		result.AmountUsedAfter = subscription.AmountUsed
		result.PlanID = plan.Id
		result.PlanTitle = plan.Title
		result.CreditPackGrantedQuota = subscription.CreditPackGrantedQuota
		result.CreditPackPaidQuota = subscription.CreditPackPaidQuota
		result.CreditPackChannelShareBPS = subscription.CreditPackChannelShareBPS
		if reservation != nil {
			result.ReservationID = reservation.ReservationID
			result.AccountID = reservation.AccountID
		}
		return nil
	})
	if errors.Is(err, errSubscriptionCandidateUnavailable) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return result, true, nil
}

func subscriptionCanPreConsume(tx *gorm.DB, subscription *commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan, modelName string, amount int64) (bool, *billingschema.BillingAccount, error) {
	var lockedLedgerAccount *billingschema.BillingAccount
	if subscription.AmountTotal > 0 {
		available, ledgerBacked, account, err := subscriptionLedgerAvailableQuotaTx(tx, subscription)
		if err != nil {
			return false, nil, err
		}
		if (ledgerBacked && available < amount) || (!ledgerBacked && subscription.AmountTotal-subscription.AmountUsed < amount) {
			return false, nil, nil
		}
		lockedLedgerAccount = account
	}
	periodAmount := getSubscriptionPeriodAmount(plan, subscription)
	if !usesLegacySubscriptionPeriodicQuota(plan, subscription) && periodAmount > 0 && periodAmount-subscription.PeriodUsed < amount {
		return false, nil, nil
	}
	if modelName == "" {
		return true, lockedLedgerAccount, nil
	}
	modelLimit, hasLimit := subscription.GetModelLimitsMap()[modelName]
	if !hasLimit || modelLimit <= 0 {
		return true, lockedLedgerAccount, nil
	}
	return subscription.GetModelUsageMap()[modelName]+amount <= modelLimit, lockedLedgerAccount, nil
}

func subscriptionPreConsumeResultFromRecord(tx *gorm.DB, record *commerceschema.SubscriptionPreConsumeRecord) (*commercedomain.SubscriptionPreConsumeResult, error) {
	if record == nil || record.UserSubscriptionId <= 0 {
		return nil, errors.New("invalid subscription pre-consume record")
	}
	var subscription commerceschema.UserSubscription
	if err := tx.Where("id = ?", record.UserSubscriptionId).First(&subscription).Error; err != nil {
		return nil, err
	}
	result := &commercedomain.SubscriptionPreConsumeResult{
		UserSubscriptionId:        subscription.Id,
		PreConsumed:               record.PreConsumed,
		AmountTotal:               subscription.AmountTotal,
		AmountUsedBefore:          subscription.AmountUsed,
		AmountUsedAfter:           subscription.AmountUsed,
		CreditPackGrantedQuota:    subscription.CreditPackGrantedQuota,
		CreditPackPaidQuota:       subscription.CreditPackPaidQuota,
		CreditPackChannelShareBPS: subscription.CreditPackChannelShareBPS,
	}
	plan, err := getSubscriptionPlanRecordTx(tx, subscription.PlanId)
	if err != nil {
		return nil, err
	}
	result.PlanID, result.PlanTitle = plan.Id, plan.Title
	reservation, err := findSubscriptionReservationTx(tx, record.RequestId)
	if err != nil {
		return nil, err
	}
	if reservation != nil {
		result.ReservationID, result.AccountID = reservation.ReservationID, reservation.AccountID
	}
	return result, nil
}
