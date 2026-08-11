package app

import (
	"fmt"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// prepareBlindBoxSubscriptionRewardTx preserves an active monthly package and
// merges the blind-box quota into it. A blind-box Lite reward must not downgrade
// an existing Standard, Pro, or Ultra subscription.
func prepareBlindBoxSubscriptionRewardTx(tx *gorm.DB, userID int, plan *commerceschema.SubscriptionPlan) (*commerceschema.UserSubscription, bool, error) {
	activeSub, _, err := pickPrimaryActivePackageTx(tx, userID, platformruntime.GetTimestamp())
	if err != nil {
		return nil, false, err
	}
	if activeSub == nil {
		subscription, err := CreateUserSubscriptionFromPlanTx(tx, userID, plan, "blind_box")
		return subscription, false, err
	}

	var locked commerceschema.UserSubscription
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", activeSub.Id).First(&locked).Error
	if err != nil {
		return nil, false, err
	}
	return &locked, true, nil
}

func creditBlindBoxSubscriptionQuotaTx(tx *gorm.DB, subscription *commerceschema.UserSubscription, amount int64, recordID int) error {
	if tx == nil || subscription == nil || amount <= 0 || recordID <= 0 {
		return nil
	}

	subscription.AmountTotal += amount
	if err := tx.Model(&commerceschema.UserSubscription{}).Where("id = ?", subscription.Id).Updates(map[string]any{
		"amount_total": subscription.AmountTotal,
		"updated_at":   platformruntime.GetTimestamp(),
	}).Error; err != nil {
		return err
	}

	var account billingschema.BillingAccount
	err := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?", "subscription", "user_subscription", subscription.Id, "quota").First(&account).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID:      account.AccountID,
		Amount:         amount,
		IdempotencyKey: fmt.Sprintf("blind-box-subscription:%d", recordID),
		ReasonCode:     "blind_box_subscription",
		ReasonDetail:   "blind-box monthly subscription reward merged into active subscription",
		ReferenceType:  "blind_box_open_record",
		ReferenceID:    fmt.Sprintf("%d", recordID),
		OperatorType:   "commerce",
		OperatorID:     "blind_box",
	})
	return err
}
