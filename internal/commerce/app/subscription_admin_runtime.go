package app

import (
	"errors"
	"fmt"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

func bindAdminSubscription(userID int, planID int) (string, error) {
	if userID <= 0 || planID <= 0 {
		return "", errors.New("invalid userId or planId")
	}

	plan, err := GetSubscriptionPlanByID(planID)
	if err != nil {
		return "", err
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		_, createErr := CreateUserSubscriptionFromPlanTx(tx, userID, plan, "admin")
		return createErr
	}); err != nil {
		return "", err
	}

	if strings.TrimSpace(plan.UpgradeGroup) != "" {
		_ = identitystore.UpdateUserGroupCache(userID, plan.UpgradeGroup)
		return fmt.Sprintf("用户分组将升级到 %s", plan.UpgradeGroup), nil
	}
	return "", nil
}

func updateAdminUserSubscriptionRuntime(userSubscriptionID int, input adminUpdateUserSubscriptionRuntimeInput) (string, error) {
	if userSubscriptionID <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}

	now := platformruntime.GetTimestamp()
	cacheGroup := ""
	var userID int
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userSubscriptionID).First(sub).Error; err != nil {
			return err
		}
		userID = sub.UserId

		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil {
			return err
		}
		if err := adjustAdminSubscriptionQuotaTx(tx, sub, input.AmountTotal, input.AmountUsed); err != nil {
			return err
		}

		sub.StartTime = input.StartTime
		sub.EndTime = input.EndTime
		sub.AmountTotal = input.AmountTotal
		sub.AmountUsed = input.AmountUsed
		sub.PeriodAmount = input.PeriodAmount
		sub.PeriodUsed = input.PeriodUsed
		sub.ModelLimits = input.ModelLimits

		if commercedomain.NormalizeResetPeriod(plan.QuotaResetPeriod) == commerceschema.SubscriptionResetNever {
			sub.LastResetTime = 0
			sub.NextResetTime = 0
		} else if sub.LastResetTime < sub.StartTime {
			sub.LastResetTime = sub.StartTime
			sub.NextResetTime = calcNextSubscriptionResetTime(time.Unix(sub.LastResetTime, 0), plan, sub.EndTime)
		}

		status := strings.TrimSpace(input.Status)
		if status == "" {
			status = sub.Status
		}
		if status == "active" && sub.EndTime > 0 && sub.EndTime <= now {
			status = "expired"
		}
		sub.Status = status

		if err := maybeResetUserSubscriptionWithPlanTx(tx, sub, plan, now); err != nil {
			return err
		}
		if err := tx.Save(sub).Error; err != nil {
			return err
		}

		upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
		switch {
		case sub.Status == "active" && sub.EndTime > now && upgradeGroup != "":
			currentGroup, err := getUserGroupByIDTx(tx, sub.UserId)
			if err != nil {
				return err
			}
			if currentGroup != upgradeGroup {
				if strings.TrimSpace(sub.PrevUserGroup) == "" {
					sub.PrevUserGroup = currentGroup
					if err := tx.Model(sub).Update("prev_user_group", sub.PrevUserGroup).Error; err != nil {
						return err
					}
				}
				if err := tx.Model(&identityschema.User{}).Where("id = ?", sub.UserId).Update("group", upgradeGroup).Error; err != nil {
					return err
				}
				cacheGroup = upgradeGroup
			}
		case sub.Status != "active" || sub.EndTime <= now:
			target, err := downgradeUserGroupForSubscriptionTx(tx, sub, now)
			if err != nil {
				return err
			}
			if target != "" {
				cacheGroup = target
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userID > 0 {
		_ = identitystore.UpdateUserGroupCache(userID, cacheGroup)
		return fmt.Sprintf("用户分组已同步为 %s", cacheGroup), nil
	}
	return "", nil
}

func invalidateAdminUserSubscriptionRuntime(userSubscriptionID int) (string, error) {
	if userSubscriptionID <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}

	now := platformruntime.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userID int
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userSubscriptionID).First(sub).Error; err != nil {
			return err
		}
		userID = sub.UserId
		if err := tx.Model(sub).Updates(map[string]any{
			"status":     "cancelled",
			"end_time":   now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		target, err := downgradeUserGroupForSubscriptionTx(tx, sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userID > 0 {
		_ = identitystore.UpdateUserGroupCache(userID, cacheGroup)
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

func deleteAdminUserSubscriptionRuntime(userSubscriptionID int) (string, error) {
	if userSubscriptionID <= 0 {
		return "", errors.New("invalid userSubscriptionId")
	}

	now := platformruntime.GetTimestamp()
	cacheGroup := ""
	downgradeGroup := ""
	var userID int
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userSubscriptionID).First(sub).Error; err != nil {
			return err
		}
		userID = sub.UserId
		target, err := downgradeUserGroupForSubscriptionTx(tx, sub, now)
		if err != nil {
			return err
		}
		if target != "" {
			cacheGroup = target
			downgradeGroup = target
		}
		return tx.Where("id = ?", userSubscriptionID).Delete(&commerceschema.UserSubscription{}).Error
	})
	if err != nil {
		return "", err
	}
	if cacheGroup != "" && userID > 0 {
		_ = identitystore.UpdateUserGroupCache(userID, cacheGroup)
	}
	if downgradeGroup != "" {
		return fmt.Sprintf("用户分组将回退到 %s", downgradeGroup), nil
	}
	return "", nil
}

func resetAdminUserSubscriptionQuotaRuntime(userSubscriptionID int, input adminResetUserSubscriptionQuotaRuntimeInput) (*commerceschema.UserSubscription, error) {
	if userSubscriptionID <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}

	now := platformruntime.GetTimestamp()
	var updated commerceschema.UserSubscription
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		sub := &commerceschema.UserSubscription{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userSubscriptionID).First(sub).Error; err != nil {
			return err
		}
		if sub.CreditPackGrantedQuota > 0 {
			return errors.New("permanent credit packs cannot be refreshed")
		}

		plan, err := getSubscriptionPlanRecordTx(tx, sub.PlanId)
		if err != nil {
			return err
		}

		sub.AmountUsed = 0
		sub.PeriodUsed = 0
		sub.ModelUsage = ""
		if err := restoreSubscriptionLedgerBalanceAfterResetTx(tx, sub, fmt.Sprintf("admin:%d:%d", sub.Id, now)); err != nil {
			return err
		}
		if commercedomain.NormalizeResetPeriod(plan.QuotaResetPeriod) == commerceschema.SubscriptionResetNever {
			sub.LastResetTime = 0
			sub.NextResetTime = 0
		} else {
			if input.AdvanceResetTime {
				sub.LastResetTime = now
				sub.NextResetTime = calcNextSubscriptionResetTime(time.Unix(now, 0), plan, sub.EndTime)
			} else if sub.LastResetTime <= 0 {
				sub.LastResetTime = maxInt64(sub.StartTime, now)
				sub.NextResetTime = calcNextSubscriptionResetTime(time.Unix(sub.LastResetTime, 0), plan, sub.EndTime)
			}
		}

		if err := tx.Save(sub).Error; err != nil {
			return err
		}
		updated = *sub
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// The caller holds the subscription lock. AmountUsed includes quota held by
// in-flight requests, so total-used is the available target, without subtracting
// reserved funds again. Reservations and immutable purchase snapshots stay intact.
func adjustAdminSubscriptionQuotaTx(tx *gorm.DB, sub *commerceschema.UserSubscription, total, used int64) error {
	if total < 0 || used < 0 || (total > 0 && used > total) {
		return errors.New("invalid subscription quota")
	}
	if sub.CreditPackGrantedQuota > 0 && total != sub.AmountTotal {
		return errors.New("permanent credit pack purchased quota cannot be changed")
	}
	if (total == 0) != (sub.AmountTotal == 0) {
		return errors.New("switching between limited and unlimited subscription quota is unsupported")
	}
	if total == 0 {
		return nil
	}
	if total == sub.AmountTotal && used == sub.AmountUsed {
		// Metadata-only edits keep an unmaterialized legacy balance untouched.
		// Existing canonical balances still need to match the absolute target.
		var existing billingschema.BillingAccount
		query := tx.Where("account_type = ? AND owner_type = ? AND owner_id = ? AND quota_unit = ?", "subscription", "user_subscription", sub.Id, "quota").Limit(1).Find(&existing)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected == 0 {
			return nil
		}
		backed, err := subscriptionLedgerHasEntriesTx(tx, existing.AccountID)
		if err != nil || !backed {
			return err
		}
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
	})
	if err != nil {
		return err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return err
	}
	if used < snapshot.ReservedBalance {
		return errors.New("amount_used cannot be less than reserved subscription quota")
	}
	ledgerBacked, err := subscriptionLedgerHasEntriesTx(tx, account.AccountID)
	if err != nil {
		return err
	}
	if !ledgerBacked {
		if snapshot.AvailableBalance != 0 || snapshot.ReservedBalance != 0 || snapshot.GrantedTotal != 0 || snapshot.ConsumedTotal != 0 || snapshot.RefundedTotal != 0 {
			return errors.New("subscription balance without ledger entries requires reconciliation")
		}
		if sub.AmountTotal <= 0 || sub.AmountUsed < 0 || sub.AmountUsed > sub.AmountTotal {
			return errors.New("invalid existing subscription quota")
		}
		available := sub.AmountTotal - sub.AmountUsed
		if available < 0 {
			return errors.New("invalid existing subscription quota")
		}
		bootstrapKey := fmt.Sprintf("subscription-bootstrap:%d", sub.Id)
		if available > 0 {
			if _, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
				AccountID: account.AccountID, Amount: available, IdempotencyKey: bootstrapKey,
				ReasonCode: "subscription_balance_bootstrap", ReferenceType: "user_subscription",
				ReferenceID: fmt.Sprintf("%d", sub.Id), OperatorType: "subscription_admin",
			}); err != nil {
				return err
			}
		} else if _, err := billingdomain.RecordAdjustmentTx(tx, billingdomain.RecordAdjustmentParams{
			AccountID: account.AccountID, IdempotencyKey: bootstrapKey,
			ReasonCode: "subscription_balance_bootstrap", ReferenceType: "user_subscription",
			ReferenceID: fmt.Sprintf("%d", sub.Id), OperatorType: "subscription_admin",
		}); err != nil {
			return err
		}
		snapshot.AvailableBalance = available
	}
	if snapshot.AvailableBalance == total-used {
		return nil
	}
	// Each committed correction gets its own receipt: a later A -> B -> A change
	// must remain possible. Repeating the canonical target does not add a write.
	_, err = billingdomain.AdjustAvailableBalanceTx(tx, billingdomain.AdjustAvailableBalanceParams{
		AccountID: account.AccountID, UsageAmount: snapshot.AvailableBalance - (total - used),
		IdempotencyKey: "subscription-admin-quota:" + platformruntime.GetUUID(),
		ReasonCode:     "subscription_admin_quota_adjustment", ReferenceType: "user_subscription",
		ReferenceID: fmt.Sprintf("%d", sub.Id),
	})
	return err
}
