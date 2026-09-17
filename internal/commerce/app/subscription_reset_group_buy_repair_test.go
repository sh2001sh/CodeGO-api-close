package app

import (
	"fmt"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
)

func TestRepairSubscriptionResetGroupBuyBonusIsExactAndIdempotent(t *testing.T) {
	db := setupRedemptionTestDB(t)
	insertSubscriptionResetAppTestUser(t, 7231, 0)
	plan := insertSubscriptionResetAppTestPlan(t, 7231, 0, 1000)
	now := time.Now().Unix()
	sub := &commerceschema.UserSubscription{
		Id: 7331, UserId: 7231, PlanId: plan.Id, AmountTotal: 1500, AmountUsed: 1500,
		StartTime: now - 3600, EndTime: now + 86400, Status: "active",
	}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&commerceschema.GroupBuyMember{
		GroupBuyId: 9931, UserId: sub.UserId, UserSubscriptionId: sub.Id,
		BonusGranted: true, BonusAmountUSD: quotaUnitsToUSD(200),
	}).Error)
	require.NoError(t, restoreSubscriptionLedgerBalanceAfterResetTx(db, sub, "repair-initial"))

	// Reproduce the old behavior: only the 1,000 base quota was restored.
	require.NoError(t, db.Model(sub).Update("amount_used", 500).Error)
	sub.AmountUsed = 500
	usedMonth := time.Now().Format("2006-01")
	require.NoError(t, restoreSubscriptionLedgerBalanceAfterResetTx(db, sub, fmt.Sprintf("opportunity:%d:%s", sub.UserId, usedMonth)))
	resetLedger := commerceschema.SubscriptionResetOpportunityLedger{
		UserId: sub.UserId, RelatedUserId: sub.Id,
		ChangeType: commerceschema.SubscriptionResetOpportunityChangeUse, Delta: -1,
		UsedMonth: usedMonth, SourceType: "user_subscription", SourceRef: fmt.Sprintf("%d", sub.Id),
		EventKey: fmt.Sprintf("use-reset-opportunity:%d:%s", sub.UserId, usedMonth),
	}
	require.NoError(t, db.Create(&resetLedger).Error)

	restored, reason, err := repairSubscriptionResetGroupBuyBonus(resetLedger)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.EqualValues(t, 200, restored)

	var current commerceschema.UserSubscription
	require.NoError(t, db.First(&current, sub.Id).Error)
	require.EqualValues(t, 300, current.AmountUsed)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	require.EqualValues(t, 1200, snapshot.AvailableBalance)

	restored, reason, err = repairSubscriptionResetGroupBuyBonus(resetLedger)
	require.NoError(t, err)
	require.Zero(t, restored)
	require.Equal(t, "already_repaired", reason)
}
