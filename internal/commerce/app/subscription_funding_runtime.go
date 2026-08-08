package app

import (
	"errors"
	"fmt"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"strings"

	commercestore "github.com/sh2001sh/new-api/internal/commerce/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// errSubscriptionCandidateUnavailable is intentionally retryable at the
// subscription-selection layer. It means this candidate was exhausted or was
// concurrently consumed; it is not a database or billing failure.
var errSubscriptionCandidateUnavailable = errors.New("subscription candidate unavailable")

// PreConsumeUserSubscription pre-consumes quota from an active subscription.
func PreConsumeUserSubscription(requestID string, userID int, modelName string, amount int64) (*commercedomain.SubscriptionPreConsumeResult, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}
	if strings.TrimSpace(requestID) == "" {
		return nil, errors.New("requestId is empty")
	}
	if amount <= 0 {
		return nil, errors.New("amount must be > 0")
	}

	now := commercestore.GetDBTimestamp()
	// Idempotency is checked before candidate discovery so retries do not touch
	// subscription rows at all.
	if result, found, err := loadSubscriptionPreConsumeResult(platformdb.DB, requestID); err != nil || found {
		return result, err
	}

	var subs []commerceschema.UserSubscription
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", now).
		Order("end_time asc, id asc").Find(&subs).Error; err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, errors.New("no active subscription")
	}
	ordered, err := orderActiveUserSubscriptions(userID, subs)
	if err != nil {
		return nil, err
	}

	for _, candidate := range ordered {
		result := &commercedomain.SubscriptionPreConsumeResult{}
		err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			return preConsumeSubscriptionCandidate(tx, requestID, userID, modelName, amount, now, candidate, result)
		})
		if err == nil {
			return result, nil
		}
		if errors.Is(err, errSubscriptionCandidateUnavailable) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("subscription quota insufficient, need=%d", amount)
}

// preConsumeSubscriptionCandidate performs one short, atomic debit attempt.
// Candidate discovery stays outside this transaction so only the exact
// subscription and its ledger snapshot are serialized by the database.
func preConsumeSubscriptionCandidate(tx *gorm.DB, requestID string, userID int, modelName string, amount int64, now int64, candidate commerceschema.UserSubscription, result *commercedomain.SubscriptionPreConsumeResult) error {
	if existingResult, found, err := loadSubscriptionPreConsumeResult(tx, requestID); err != nil || found {
		if found {
			*result = *existingResult
		}
		return err
	}

	sub := &commerceschema.UserSubscription{}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", candidate.Id, userID).First(sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errSubscriptionCandidateUnavailable
		}
		return err
	}
	if sub.Status != "active" || sub.EndTime <= now {
		return errSubscriptionCandidateUnavailable
	}
	plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
	if err != nil {
		return err
	}
	if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
		return err
	}
	hasQuota, err := subscriptionHasQuotaForRequest(tx, sub, plan, modelName, amount)
	if err != nil {
		return err
	}
	if !hasQuota {
		return errSubscriptionCandidateUnavailable
	}

	usedBefore := sub.AmountUsed
	trimmedModelName := strings.TrimSpace(modelName)
	record := &commerceschema.SubscriptionPreConsumeRecord{
		RequestId: requestID, UserId: userID, UserSubscriptionId: sub.Id,
		ModelName: trimmedModelName, PreConsumed: amount, Status: "consumed",
	}
	createResult := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(record)
	if createResult.Error != nil {
		return createResult.Error
	}
	if createResult.RowsAffected == 0 {
		existingResult, found, err := loadSubscriptionPreConsumeResult(tx, requestID)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("subscription pre-consume idempotency record unavailable")
		}
		*result = *existingResult
		return nil
	}
	if err := reserveSubscriptionLedgerTx(tx, sub, record); err != nil {
		if errors.Is(err, billingdomain.ErrInsufficientBalance) {
			return errSubscriptionCandidateUnavailable
		}
		return err
	}
	if err := applySubscriptionUsageDelta(plan, sub, record.ModelName, amount); err != nil {
		return err
	}
	if err := tx.Save(sub).Error; err != nil {
		return err
	}
	result.UserSubscriptionId = sub.Id
	result.PreConsumed = amount
	result.AmountTotal = sub.AmountTotal
	result.AmountUsedBefore = usedBefore
	result.AmountUsedAfter = sub.AmountUsed
	return nil
}

func loadSubscriptionPreConsumeResult(db *gorm.DB, requestID string) (*commercedomain.SubscriptionPreConsumeResult, bool, error) {
	record := &commerceschema.SubscriptionPreConsumeRecord{}
	query := db.Where("request_id = ?", requestID).Limit(1).Find(record)
	if query.Error != nil {
		return nil, false, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, false, nil
	}
	if record.Status == "refunded" {
		return nil, true, errors.New("subscription pre-consume already refunded")
	}
	sub := &commerceschema.UserSubscription{}
	if err := db.Where("id = ?", record.UserSubscriptionId).First(sub).Error; err != nil {
		return nil, true, err
	}
	return &commercedomain.SubscriptionPreConsumeResult{
		UserSubscriptionId: sub.Id, PreConsumed: record.PreConsumed, AmountTotal: sub.AmountTotal,
		AmountUsedBefore: sub.AmountUsed, AmountUsedAfter: sub.AmountUsed,
	}, true, nil
}

func subscriptionHasQuotaForRequest(tx *gorm.DB, sub *commerceschema.UserSubscription, plan *commerceschema.SubscriptionPlan, modelName string, amount int64) (bool, error) {
	if sub.AmountTotal > 0 {
		available, ledgerBacked, err := subscriptionLedgerAvailableQuotaTx(tx, sub)
		if err != nil {
			return false, err
		}
		if (ledgerBacked && available < amount) || (!ledgerBacked && sub.AmountTotal-sub.AmountUsed < amount) {
			return false, nil
		}
	}
	periodAmount := getSubscriptionPeriodAmount(plan, sub)
	if !usesLegacySubscriptionPeriodicQuota(plan, sub) && periodAmount > 0 && periodAmount-sub.PeriodUsed < amount {
		return false, nil
	}
	trimmedModelName := strings.TrimSpace(modelName)
	if trimmedModelName != "" {
		modelLimits := sub.GetModelLimitsMap()
		if modelLimit, ok := modelLimits[trimmedModelName]; ok && modelLimit > 0 && sub.GetModelUsageMap()[trimmedModelName]+amount > modelLimit {
			return false, nil
		}
	}
	return true, nil
}

func reserveSubscriptionLedgerTx(tx *gorm.DB, sub *commerceschema.UserSubscription, record *commerceschema.SubscriptionPreConsumeRecord) error {
	if sub == nil || record == nil || sub.AmountTotal <= 0 {
		return nil
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

	var entryCount int64
	if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&entryCount).Error; err != nil {
		return err
	}
	if entryCount == 0 {
		available := sub.AmountTotal - sub.AmountUsed
		if available > 0 {
			if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
				AccountID:      account.AccountID,
				Amount:         available,
				IdempotencyKey: fmt.Sprintf("subscription-bootstrap:%d", sub.Id),
				ReasonCode:     "subscription_balance_bootstrap",
				ReferenceType:  "user_subscription",
				ReferenceID:    fmt.Sprintf("%d", sub.Id),
				OperatorType:   "subscription_projection",
				OperatorID:     record.RequestId,
			}); err != nil {
				return err
			}
		}
	}
	_, err = billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
		AccountID:      account.AccountID,
		RequestID:      record.RequestId,
		ReservedAmount: record.PreConsumed,
		IdempotencyKey: "subscription:" + record.RequestId + ":reserve",
	})
	return err
}

// ReserveAdditionalSubscriptionQuota reserves a confirmed extra amount for a request.
// The subscription fields remain a query projection; ledger reservations enforce balance.
func ReserveAdditionalSubscriptionQuota(requestID string, subscriptionID int, modelName string, amount int64) error {
	if strings.TrimSpace(requestID) == "" || subscriptionID <= 0 || amount <= 0 {
		return errors.New("requestId, subscriptionId and amount are required")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", subscriptionID).First(sub).Error; err != nil {
			return err
		}
		now := commercestore.GetDBTimestamp()
		if sub.Status != "active" || sub.EndTime <= now {
			return errors.New("subscription is no longer active")
		}
		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil {
			return err
		}
		if err := applySubscriptionUsageDelta(plan, sub, modelName, amount); err != nil {
			return err
		}
		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscriptionID), QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		_, err = billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
			AccountID: account.AccountID, RequestID: requestID, ReservedAmount: amount,
			IdempotencyKey: fmt.Sprintf("subscription:%s:reserve-extra:%d", requestID, amount),
		})
		return err
	})
}

// RefundSubscriptionPreConsume refunds a previous subscription pre-consume idempotently.
func RefundSubscriptionPreConsume(requestID string) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("requestId is empty")
	}

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		record := &commerceschema.SubscriptionPreConsumeRecord{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("request_id = ?", requestID).
			First(record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		if record.PreConsumed <= 0 {
			record.Status = "refunded"
			return tx.Save(record).Error
		}
		if err := lockUserSubscriptionTx(tx, record.UserSubscriptionId); err != nil {
			return err
		}
		releasedAmount, err := releaseSubscriptionReservationTx(tx, record)
		if err != nil {
			return err
		}
		if err := postConsumeUserSubscriptionUsageDeltaTx(tx, record.UserSubscriptionId, record.ModelName, -releasedAmount); err != nil {
			return err
		}
		record.Status = "refunded"
		return tx.Save(record).Error
	})
}
