package settlement

import (
	"errors"
	"fmt"
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDirectReclaimCommitsDebitBeforeHistoryAndResumesWithoutDebitingAgain(t *testing.T) {
	db := openSettlementTestDB(t)
	rows := make([]marketplaceschema.Settlement, reclaimBatchSize+1)
	for i := range rows {
		rows[i] = postgresReclaimSettlement(fmt.Sprintf("direct-%05d", i), 10, 1, time.Now().UTC())
	}
	require.NoError(t, db.CreateInBatches(rows, 500).Error)
	RegisterReclaimBalancesHook(func(_ *gorm.DB, _ []int) (map[int]int64, error) { return map[int]int64{10: reclaimBatchSize + 1}, nil })
	calls := 0
	RegisterReclaimHook(func(_ *gorm.DB, _, _ int, amount int64, key string) error {
		calls++
		require.EqualValues(t, reclaimBatchSize+1, amount)
		require.Equal(t, "marketplace-reclaim:direct-retry:direct:owner:10", key)
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil); RegisterReclaimBalancesHook(nil) })
	filter := ReleaseFilter{OwnerUserIDs: []int{10}, OperationID: "direct-retry", Direct: true}
	task, err := CreateIncomeReclaimTask(filter)
	require.NoError(t, err)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	task, err = GetIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, reclaimTaskCharged, task.Status)
	require.EqualValues(t, reclaimBatchSize+1, task.Amount)
	var audited int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("reclaimed_amount > 0").Count(&audited).Error)
	require.Zero(t, audited)
	_, err = CreateIncomeReclaimTask(ReleaseFilter{OwnerUserIDs: []int{10}, OperationID: "overlap", Direct: true})
	require.ErrorContains(t, err, "勿重复提交")
	replay, err := CreateIncomeReclaimTask(filter)
	require.NoError(t, err)
	require.Equal(t, task.Amount, replay.Amount)
	_, err = CreateIncomeReclaimTask(ReleaseFilter{OwnerUserIDs: []int{10}, OperationID: filter.OperationID, MaxAmount: 2, Direct: true})
	require.ErrorContains(t, err, "其他筛选条件")
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	var partial marketplaceschema.IncomeReclaim
	require.NoError(t, db.First(&partial, "id = ?", task.ID).Error)
	require.Equal(t, reclaimTaskCharged, partial.Status)
	require.Equal(t, reclaimBatchSize, partial.Count)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	task, err = GetIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, reclaimTaskCompleted, task.Status)
	require.Equal(t, len(rows), task.Count)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("reclaimed_amount = 1 AND status = ?", statusReclaimed).Count(&audited).Error)
	require.EqualValues(t, len(rows), audited)
}

func TestDirectReclaimCapsAvailableFundsAndRetainsChargedAuditOnFailure(t *testing.T) {
	db := openSettlementTestDB(t)
	reference := time.Now().Truncate(time.Second)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{
		postgresReclaimSettlement("a", 10, 50, reference), postgresReclaimSettlement("b", 11, 40, reference),
		postgresReclaimSettlement("old", 10, 999, reference.Add(-2*time.Hour)),
	}).Error)
	RegisterReclaimBalancesHook(func(_ *gorm.DB, _ []int) (map[int]int64, error) { return map[int]int64{10: 20, 11: -10}, nil })
	calls := 0
	RegisterReclaimHook(func(_ *gorm.DB, owner, _ int, amount int64, _ string) error {
		calls++
		require.Equal(t, 10, owner)
		require.EqualValues(t, 20, amount)
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil); RegisterReclaimBalancesHook(nil) })
	task, err := CreateIncomeReclaimTask(ReleaseFilter{Direct: true, OwnerUserIDs: []int{10, 11}, StartTimestamp: reference.Unix(), EndTimestamp: reference.Unix(), OperationID: "caps"})
	require.NoError(t, err)
	task, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.EqualValues(t, 20, task.Amount)
	require.NoError(t, db.Exec("CREATE TRIGGER audit_failure BEFORE UPDATE ON marketplace_settlements BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END").Error)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.ErrorContains(t, err, "forced audit failure")
	task, err = GetIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, reclaimTaskCharged, task.Status)
	require.EqualValues(t, 20, task.Amount)
	require.NotEmpty(t, task.ErrorMessage)
	require.NoError(t, db.Exec("DROP TRIGGER audit_failure").Error)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	var first marketplaceschema.Settlement
	require.NoError(t, db.First(&first, "request_id = ?", "a").Error)
	require.EqualValues(t, 20, first.ReclaimedAmount)
	require.Equal(t, statusReleased, first.Status)
}

func TestDirectReclaimWalletFailureRollsBackReceiptAndHistory(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, db.Create(&[]marketplaceschema.Settlement{postgresReclaimSettlement("fail-a", 10, 50, time.Now()), postgresReclaimSettlement("fail-b", 11, 40, time.Now())}).Error)
	RegisterReclaimBalancesHook(func(_ *gorm.DB, _ []int) (map[int]int64, error) { return map[int]int64{10: 50, 11: 40}, nil })
	RegisterReclaimHook(func(_ *gorm.DB, owner, _ int, _ int64, _ string) error {
		if owner == 11 {
			return errors.New("forced wallet failure")
		}
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil); RegisterReclaimBalancesHook(nil) })
	task, err := CreateIncomeReclaimTask(ReleaseFilter{Direct: true, OwnerUserIDs: []int{10, 11}, OperationID: "failed-debit"})
	require.NoError(t, err)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.ErrorContains(t, err, "forced wallet failure")
	task, err = GetIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.Equal(t, reclaimTaskFailed, task.Status)
	require.Zero(t, task.Amount)
	var amount int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Select("sum(reclaimed_amount)").Scan(&amount).Error)
	require.Zero(t, amount)
}

func TestDirectReclaimExactAmountUsesOwnerOrderAndRejectsInsufficientTotalBeforeDebit(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, db.Create([]marketplaceschema.Settlement{postgresReclaimSettlement("exact-a", 10, 70, time.Now()), postgresReclaimSettlement("exact-b", 11, 50, time.Now())}).Error)
	RegisterReclaimBalancesHook(func(_ *gorm.DB, _ []int) (map[int]int64, error) { return map[int]int64{10: 30, 11: 50}, nil })
	transfers := map[int]int64{}
	RegisterReclaimHook(func(_ *gorm.DB, owner, _ int, amount int64, _ string) error { transfers[owner] += amount; return nil })
	t.Cleanup(func() { RegisterReclaimHook(nil); RegisterReclaimBalancesHook(nil) })
	task, err := CreateIncomeReclaimTask(ReleaseFilter{Direct: true, OwnerUserIDs: []int{11, 10}, MaxAmount: 81, OperationID: "too-much"})
	require.NoError(t, err)
	_, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.ErrorContains(t, err, "可用余额不足")
	require.Empty(t, transfers)
	task, err = CreateIncomeReclaimTask(ReleaseFilter{Direct: true, OwnerUserIDs: []int{11, 10}, MaxAmount: 45, OperationID: "exact-direct"})
	require.NoError(t, err)
	task, err = ProcessDirectIncomeReclaimTask(task.ID)
	require.NoError(t, err)
	require.EqualValues(t, 45, task.Amount)
	require.Equal(t, map[int]int64{10: 30, 11: 15}, transfers)
}

func TestDirectReclaimExcludesIncomeHeldForOwnerWithdrawal(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.OwnerWithdrawal{}))
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "held-income", RequestID: "held-income", GroupID: "g", OwnerUserID: 10,
		OwnerNetAmount: 150, Status: statusReleased,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.OwnerWithdrawal{
		ID: "held-withdrawal", OwnerUserID: 10, ReclaimOperationID: "held-withdrawal-reclaim",
		ReservationID: "held-reservation", RequestFingerprint: "held-fingerprint",
		RequestedCredits: 100, RequestedQuota: 100, RateCNYCentsPerCredit: 95,
		PayoutAmountCents: 9500, PaymentMethod: marketplaceschema.OwnerWithdrawalMethodAlipay,
		PayeeName: "owner", PaymentDetailsCiphertext: "encrypted", PaymentDetailsTail: "1234",
		Status: marketplaceschema.OwnerWithdrawalStatusPendingTransfer,
	}).Error)
	RegisterReclaimBalancesHook(func(_ *gorm.DB, _ []int) (map[int]int64, error) {
		return map[int]int64{10: 500}, nil
	})
	debited := int64(0)
	RegisterReclaimHook(func(_ *gorm.DB, _ int, _ int, amount int64, _ string) error {
		debited += amount
		return nil
	})
	t.Cleanup(func() { RegisterReclaimHook(nil); RegisterReclaimBalancesHook(nil) })

	tooMuch, err := CreateIncomeReclaimTask(ReleaseFilter{
		Direct: true, OwnerUserIDs: []int{10}, MaxAmount: 51, OperationID: "held-too-much",
	})
	require.NoError(t, err)
	_, err = ProcessDirectIncomeReclaimTask(tooMuch.ID)
	require.ErrorContains(t, err, "可用余额不足")
	require.Zero(t, debited)

	allowed, err := CreateIncomeReclaimTask(ReleaseFilter{
		Direct: true, OwnerUserIDs: []int{10}, MaxAmount: 50, OperationID: "held-allowed",
	})
	require.NoError(t, err)
	allowed, err = ProcessDirectIncomeReclaimTask(allowed.ID)
	require.NoError(t, err)
	require.EqualValues(t, 50, allowed.Amount)
	require.EqualValues(t, 50, debited)
}
