package app

import (
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func ensureSubscriptionPreConsumeRecordSchema(t *testing.T) {
	t.Helper()
	require.NoError(t, platformdb.DB.AutoMigrate(&commerceschema.SubscriptionPreConsumeRecord{}))
}

func TestPreConsumeAndRefundSubscriptionPreConsume(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)

	insertSubscriptionStoreTestUser(t, 9701, []int{9703})
	plan := insertSubscriptionResetAppTestPlan(t, 9702, 0, int64(platformruntime.QuotaPerUnit)*10)
	plan.PeriodAmount = int64(platformruntime.QuotaPerUnit) * 5
	plan.QuotaResetPeriod = commerceschema.SubscriptionResetMonthly
	require.NoError(t, db.Save(plan).Error)
	initialModelUsage, err := commercedomain.EncodeSubscriptionModelQuotaMap(map[string]int64{
		"gpt-4.1": int64(platformruntime.QuotaPerUnit / 2),
	})
	require.NoError(t, err)
	consumedModelUsage, err := commercedomain.EncodeSubscriptionModelQuotaMap(map[string]int64{
		"gpt-4.1": int64(platformruntime.QuotaPerUnit) + int64(platformruntime.QuotaPerUnit/2),
	})
	require.NoError(t, err)
	modelLimits, err := commercedomain.EncodeSubscriptionModelQuotaMap(map[string]int64{
		"gpt-4.1": int64(platformruntime.QuotaPerUnit) * 5,
	})
	require.NoError(t, err)

	sub := &commerceschema.UserSubscription{
		Id:           9703,
		UserId:       9701,
		PlanId:       plan.Id,
		AmountTotal:  plan.TotalAmount,
		AmountUsed:   int64(platformruntime.QuotaPerUnit),
		PeriodAmount: plan.PeriodAmount,
		PeriodUsed:   int64(platformruntime.QuotaPerUnit / 2),
		ModelLimits:  modelLimits,
		ModelUsage:   initialModelUsage,
		StartTime:    time.Now().Add(-24 * time.Hour).Unix(),
		EndTime:      time.Now().Add(30 * 24 * time.Hour).Unix(),
		Status:       "active",
	}
	require.NoError(t, db.Create(sub).Error)

	result, err := PreConsumeUserSubscription("refundable-preconsume", 9701, "gpt-4.1", int64(platformruntime.QuotaPerUnit))
	require.NoError(t, err)
	assert.Equal(t, sub.Id, result.UserSubscriptionId)
	assert.EqualValues(t, platformruntime.QuotaPerUnit, result.PreConsumed)
	assert.EqualValues(t, platformruntime.QuotaPerUnit, result.AmountUsedBefore)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit)*2, result.AmountUsedAfter)

	var consumed commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&consumed).Error)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit)*2, consumed.AmountUsed)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit)+int64(platformruntime.QuotaPerUnit/2), consumed.PeriodUsed)
	assert.JSONEq(t, consumedModelUsage, consumed.ModelUsage)

	duplicate, err := PreConsumeUserSubscription("refundable-preconsume", 9701, "gpt-4.1", int64(platformruntime.QuotaPerUnit))
	require.NoError(t, err)
	assert.Equal(t, result.UserSubscriptionId, duplicate.UserSubscriptionId)
	assert.Equal(t, result.PreConsumed, duplicate.PreConsumed)

	require.NoError(t, RefundSubscriptionPreConsume("refundable-preconsume"))
	require.NoError(t, RefundSubscriptionPreConsume("refundable-preconsume"))

	var refunded commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&refunded).Error)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit), refunded.AmountUsed)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit/2), refunded.PeriodUsed)
	assert.JSONEq(t, initialModelUsage, refunded.ModelUsage)

	var record commerceschema.SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", "refundable-preconsume").First(&record).Error)
	assert.Equal(t, "refunded", record.Status)
}

func TestPostConsumeUserSubscriptionUsageDelta_TracksPeriodAndModelUsage(t *testing.T) {
	db := setupRedemptionTestDB(t)

	plan := insertSubscriptionResetAppTestPlan(t, 9801, 0, int64(platformruntime.QuotaPerUnit)*10)
	plan.PeriodAmount = int64(platformruntime.QuotaPerUnit) * 3
	plan.QuotaResetPeriod = commerceschema.SubscriptionResetMonthly
	require.NoError(t, db.Save(plan).Error)

	sub := &commerceschema.UserSubscription{
		Id:           9802,
		UserId:       9801,
		PlanId:       plan.Id,
		AmountTotal:  plan.TotalAmount,
		PeriodAmount: plan.PeriodAmount,
		StartTime:    time.Now().Add(-24 * time.Hour).Unix(),
		EndTime:      time.Now().Add(30 * 24 * time.Hour).Unix(),
		Status:       "active",
	}
	require.NoError(t, db.Create(sub).Error)
	expectedModelUsage, err := commercedomain.EncodeSubscriptionModelQuotaMap(map[string]int64{
		"gpt-4.1": int64(platformruntime.QuotaPerUnit / 2),
	})
	require.NoError(t, err)
	modelLimits, err := commercedomain.EncodeSubscriptionModelQuotaMap(map[string]int64{
		"gpt-4.1": int64(platformruntime.QuotaPerUnit) * 5,
	})
	require.NoError(t, err)
	sub.ModelLimits = modelLimits
	require.NoError(t, db.Save(sub).Error)

	require.NoError(t, PostConsumeUserSubscriptionUsageDelta(sub.Id, "gpt-4.1", int64(platformruntime.QuotaPerUnit)))
	require.NoError(t, PostConsumeUserSubscriptionUsageDelta(sub.Id, "gpt-4.1", -int64(platformruntime.QuotaPerUnit/2)))

	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&reloaded).Error)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit/2), reloaded.AmountUsed)
	assert.EqualValues(t, int64(platformruntime.QuotaPerUnit/2), reloaded.PeriodUsed)
	assert.JSONEq(t, expectedModelUsage, reloaded.ModelUsage)
}

func TestSubscriptionLedgerSettlesAdditionalReservationAndProjectsUsage(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)

	insertSubscriptionStoreTestUser(t, 9901, []int{9902})
	plan := insertSubscriptionResetAppTestPlan(t, 9903, 0, 10_000)
	subscription := &commerceschema.UserSubscription{
		Id: 9902, UserId: 9901, PlanId: plan.Id, AmountTotal: 10_000,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(subscription).Error)

	_, err := PreConsumeUserSubscription("subscription-ledger-settle", 9901, "gpt-5", 1_000)
	require.NoError(t, err)
	require.NoError(t, ReserveAdditionalSubscriptionQuota("subscription-ledger-settle", subscription.Id, "gpt-5", 500))
	require.NoError(t, SettleSubscriptionReservation("subscription-ledger-settle", subscription.Id, "gpt-5", 1_200))
	require.NoError(t, SettleSubscriptionReservation("subscription-ledger-settle", subscription.Id, "gpt-5", 1_200))

	var reservations []billingschema.BillingReservation
	require.NoError(t, db.Where("request_id = ?", "subscription-ledger-settle").Find(&reservations).Error)
	require.Len(t, reservations, 2)
	for _, reservation := range reservations {
		require.Equal(t, billingschema.BillingReservationStatusSettled, reservation.Status)
	}
	var settledTotal int64
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Select("COALESCE(SUM(actual_amount), 0)").Scan(&settledTotal).Error)
	require.EqualValues(t, 1_200, settledTotal)

	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", subscription.Id).First(&reloaded).Error)
	require.EqualValues(t, 1_200, reloaded.AmountUsed)
}

func TestPreConsumeUserSubscriptionRejectsExpiredAndExhaustedSubscriptions(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionStoreTestUser(t, 9911, []int{9912, 9913})
	plan := insertSubscriptionResetAppTestPlan(t, 9914, 0, 1_000)

	expired := &commerceschema.UserSubscription{
		Id: 9912, UserId: 9911, PlanId: plan.Id, AmountTotal: 1_000,
		StartTime: time.Now().Add(-48 * time.Hour).Unix(), EndTime: time.Now().Add(-time.Minute).Unix(), Status: "active",
	}
	exhausted := &commerceschema.UserSubscription{
		Id: 9913, UserId: 9911, PlanId: plan.Id, AmountTotal: 1_000, AmountUsed: 1_000,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(expired).Error)
	require.NoError(t, db.Create(exhausted).Error)

	_, err := PreConsumeUserSubscription("subscription-expired", 9911, "gpt-5", 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "subscription quota insufficient")
}

func TestReserveAdditionalSubscriptionQuotaRejectsExpiredSubscription(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := insertSubscriptionResetAppTestPlan(t, 9921, 0, 1_000)
	subscription := &commerceschema.UserSubscription{
		Id: 9922, UserId: 9920, PlanId: plan.Id, AmountTotal: 1_000,
		StartTime: time.Now().Add(-48 * time.Hour).Unix(), EndTime: time.Now().Add(-time.Minute).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(subscription).Error)

	err := ReserveAdditionalSubscriptionQuota("subscription-extra-expired", subscription.Id, "gpt-5", 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no longer active")
}

func TestPreConsumeUsesLedgerBalanceWhenProjectionIsStale(t *testing.T) {
	db := setupRedemptionTestDB(t)
	ensureSubscriptionPreConsumeRecordSchema(t)
	insertSubscriptionStoreTestUser(t, 9931, []int{9932})
	plan := insertSubscriptionResetAppTestPlan(t, 9933, 0, 1_000)
	subscription := &commerceschema.UserSubscription{
		Id: 9932, UserId: 9931, PlanId: plan.Id, AmountTotal: 1_000, AmountUsed: 1_000,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(subscription).Error)

	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscription.Id), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: 1_000, IdempotencyKey: "stale-projection-ledger-credit",
		ReasonCode: "test", ReferenceType: "user_subscription", ReferenceID: "9932",
	})
	require.NoError(t, err)

	result, err := PreConsumeUserSubscription("stale-projection-request", 9931, "gpt-5", 100)
	require.NoError(t, err)
	assert.EqualValues(t, 0, result.AmountUsedBefore)
	assert.EqualValues(t, 100, result.AmountUsedAfter)

	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", subscription.Id).First(&reloaded).Error)
	assert.EqualValues(t, 100, reloaded.AmountUsed)
}

func TestReconcileActiveSubscriptionLedgerProjections(t *testing.T) {
	db := setupRedemptionTestDB(t)
	plan := insertSubscriptionResetAppTestPlan(t, 9941, 0, 1_000)
	subscription := &commerceschema.UserSubscription{
		Id: 9942, UserId: 9940, PlanId: plan.Id, AmountTotal: 1_000, AmountUsed: 900,
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active",
	}
	require.NoError(t, db.Create(subscription).Error)

	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{
		AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(subscription.Id), QuotaUnit: "quota",
	})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: 700, IdempotencyKey: "projection-reconcile-ledger-credit",
		ReasonCode: "test", ReferenceType: "user_subscription", ReferenceID: "9942",
	})
	require.NoError(t, err)

	updated, err := ReconcileActiveSubscriptionLedgerProjections(10)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)

	var reloaded commerceschema.UserSubscription
	require.NoError(t, db.Where("id = ?", subscription.Id).First(&reloaded).Error)
	assert.EqualValues(t, 300, reloaded.AmountUsed)
	var adjustment billingschema.BillingLedgerEntry
	require.NoError(t, db.Where("account_id = ? AND reason_code = ?", account.AccountID, "subscription_projection_reconciled").First(&adjustment).Error)
	assert.EqualValues(t, 0, adjustment.Amount)
}
