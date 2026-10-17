package app

import (
	"context"
	"errors"
	"testing"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func taskSubscriptionBalance(t *testing.T, subscriptionID int) int64 {
	t.Helper()
	var account billingschema.BillingAccount
	require.NoError(t, platformdb.DB.Where("account_type = ? AND owner_type = ? AND owner_id = ?", "subscription", "user_subscription", subscriptionID).First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, platformdb.DB.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	return snapshot.AvailableBalance
}

func taskAdjustmentCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingLedgerEntry{}).Where("reason_code = ?", "subscription_usage_adjustment").Count(&count).Error)
	return count
}

func TestSubscriptionTaskTerminalFundingAndReplay(t *testing.T) {
	for _, test := range []struct {
		name   string
		actual int
		status workflowschema.TaskStatus
	}{
		{name: "extra_charge", actual: 3000, status: workflowschema.TaskStatusSuccess},
		{name: "partial_refund", actual: 1000, status: workflowschema.TaskStatusSuccess},
		{name: "full_refund", actual: 0, status: workflowschema.TaskStatusFailure},
		{name: "exact_precharge", actual: 2000, status: workflowschema.TaskStatusSuccess},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncateTables(t)
			seedUser(t, 71, 0)
			seedChannel(t, 71)
			seedToken(t, 71, 71, "task-atomic-token", 10000)
			seedSubscriptionPlan(t, 71)
			seedSubscription(t, 71, 71, 71, 100000, 50000)
			task := makeTask(71, 71, 2000, 71, testBillingSourceSubscription, 71)
			require.NoError(t, platformdb.DB.Create(task).Error)
			stale := *task
			fromStatus := task.Status
			task.Status = test.status
			task.Progress = "100%"
			won, err := CommitTaskTerminalTransition(context.Background(), task, fromStatus, test.actual, "provider terminal")
			require.NoError(t, err)
			require.True(t, won)
			require.EqualValues(t, 50000+test.actual-2000, getSubscriptionUsed(t, 71))
			var reloaded workflowschema.Task
			require.NoError(t, platformdb.DB.First(&reloaded, task.ID).Error)
			require.Equal(t, test.status, reloaded.Status)
			require.Equal(t, test.actual, reloaded.Quota)
			adjustments := int64(1)
			if test.actual == 2000 {
				adjustments = 0
			} else {
				require.EqualValues(t, 50000-test.actual+2000, taskSubscriptionBalance(t, 71))
			}
			require.Equal(t, adjustments, taskAdjustmentCount(t))
			require.Equal(t, 10000-test.actual+2000, getTokenRemainQuota(t, 71))
			logs := countLogs(t)
			stale.Status = test.status
			won, err = CommitTaskTerminalTransition(context.Background(), &stale, fromStatus, test.actual, "replayed provider terminal")
			require.NoError(t, err)
			require.False(t, won, "the original terminal CAS must prevent duplicate funding and projections")
			require.Equal(t, adjustments, taskAdjustmentCount(t))
			require.Equal(t, logs, countLogs(t))
			require.Equal(t, 10000-test.actual+2000, getTokenRemainQuota(t, 71))
		})
	}
}

func TestSubscriptionTaskFundingFailureRollsBackTerminalAndCanRetry(t *testing.T) {
	truncateTables(t)
	seedUser(t, 72, 0)
	seedChannel(t, 72)
	seedToken(t, 72, 72, "task-retry-token", 10000)
	seedSubscriptionPlan(t, 72)
	seedSubscription(t, 72, 72, 72, 100000, 50000)
	task := makeTask(72, 72, 2000, 72, testBillingSourceSubscription, 72)
	require.NoError(t, platformdb.DB.Create(task).Error)
	fromStatus := task.Status
	const callback = "test:task-subscription-write-failure"
	fundingErr := errors.New("subscription usage projection unavailable")
	require.NoError(t, platformdb.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UserSubscription" {
			tx.AddError(fundingErr)
		}
	}))
	t.Cleanup(func() { _ = platformdb.DB.Callback().Update().Remove(callback) })
	task.Status = workflowschema.TaskStatusFailure
	task.Progress = "100%"
	won, err := CommitTaskTerminalTransition(context.Background(), task, fromStatus, 0, "provider failed")
	require.ErrorIs(t, err, fundingErr)
	require.False(t, won)
	var reloaded workflowschema.Task
	require.NoError(t, platformdb.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, fromStatus, reloaded.Status, "failed funds cannot strand a terminal task outside polling")
	require.Equal(t, 2000, reloaded.Quota)
	require.EqualValues(t, 50000, getSubscriptionUsed(t, 72))
	require.Zero(t, taskAdjustmentCount(t), "the inserted authoritative ledger adjustment must roll back too")
	require.Equal(t, 10000, getTokenRemainQuota(t, 72))
	require.Zero(t, countLogs(t))
	require.NoError(t, platformdb.DB.Callback().Update().Remove(callback))
	reloaded.Status = workflowschema.TaskStatusFailure
	won, err = CommitTaskTerminalTransition(context.Background(), &reloaded, fromStatus, 0, "provider failed")
	require.NoError(t, err)
	require.True(t, won)
	require.EqualValues(t, 48000, getSubscriptionUsed(t, 72))
	require.EqualValues(t, 52000, taskSubscriptionBalance(t, 72))
	require.EqualValues(t, 1, taskAdjustmentCount(t))
	require.Equal(t, 12000, getTokenRemainQuota(t, 72))
}

func TestSubscriptionTaskAdjustmentBoundsAndDirectRefundReplay(t *testing.T) {
	truncateTables(t)
	seedUser(t, 73, 0)
	seedChannel(t, 73)
	seedSubscriptionPlan(t, 73)
	seedSubscription(t, 73, 73, 73, 100000, 50000)
	task := makeTask(73, 73, 2000, 0, testBillingSourceSubscription, 73)
	require.NoError(t, platformdb.DB.Create(task).Error)
	fromStatus := task.Status
	task.Status = workflowschema.TaskStatusSuccess
	won, err := CommitTaskTerminalTransition(context.Background(), task, fromStatus, 100000, "insufficient balance")
	require.ErrorIs(t, err, billingdomain.ErrInsufficientBalance)
	require.False(t, won)
	var reloaded workflowschema.Task
	require.NoError(t, platformdb.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, fromStatus, reloaded.Status)
	require.Zero(t, taskAdjustmentCount(t))
	_, err = CommitTaskTerminalTransition(context.Background(), task, fromStatus, -1, "invalid quota")
	require.Error(t, err)
	require.NoError(t, recordTaskRefund(context.Background(), &reloaded, "manual workflow retry"))
	require.NoError(t, recordTaskRefund(context.Background(), &reloaded, "manual workflow replay"))
	require.EqualValues(t, 48000, getSubscriptionUsed(t, 73))
	require.EqualValues(t, 52000, taskSubscriptionBalance(t, 73))
	require.EqualValues(t, 1, taskAdjustmentCount(t))
	require.EqualValues(t, 1, countLogs(t))
	// Reusing one task operation with a different delta cannot silently change it.
	require.ErrorIs(t, settleTaskQuotaDelta(context.Background(), &reloaded, 3000, "conflicting replay"), billingdomain.ErrLedgerConflict)
	var sub commerceschema.UserSubscription
	require.NoError(t, platformdb.DB.First(&sub, 73).Error)
	require.EqualValues(t, 48000, sub.AmountUsed)
}

func TestSubscriptionTaskZeroQuotaTerminalNeedsNoFundingAdjustment(t *testing.T) {
	truncateTables(t)
	task := makeTask(74, 74, 0, 0, testBillingSourceSubscription, 74)
	require.NoError(t, platformdb.DB.Create(task).Error)
	fromStatus := task.Status
	task.Status = workflowschema.TaskStatusFailure
	won, err := CommitTaskTerminalTransition(context.Background(), task, fromStatus, 0, "zero quota failed task")
	require.NoError(t, err)
	require.True(t, won)
	require.Zero(t, taskAdjustmentCount(t))
	require.Zero(t, countLogs(t))
	var reloaded workflowschema.Task
	require.NoError(t, platformdb.DB.First(&reloaded, task.ID).Error)
	require.EqualValues(t, workflowschema.TaskStatusFailure, reloaded.Status)
	require.Zero(t, reloaded.Quota)
}
