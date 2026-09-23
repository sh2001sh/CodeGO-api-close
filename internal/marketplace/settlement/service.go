package settlement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	statusPending   = "pending"
	statusReleased  = "released"
	statusReclaimed = "reclaimed"
	statusForfeited = "forfeited"
)

type RecordParams struct {
	RequestID              string
	GroupID                string
	OwnerUserID            int
	ConsumerUserID         int
	BillingSource          string
	ConsumerDebitAmount    int64
	SettlementGrossAmount  int64
	WalletMultiplier       float64
	SubscriptionMultiplier float64
}

type ReleaseHook func(tx *gorm.DB, userID int, amount int64, idempotencyKey string, reasonCode string) error
type ReclaimHook func(tx *gorm.DB, ownerUserID int, adminUserID int, amount int64, idempotencyKey string) error
type ForfeitHook func(tx *gorm.DB, pendingAccountID string, adminUserID int, amount int64, idempotencyKey string) error

type ReleaseFilter struct {
	OwnerUserIDs   []int
	StartTimestamp int64
	EndTimestamp   int64
	Limit          int
	MaxAmount      int64 // Exact amount to reclaim; 0 means all matching released earnings.
	OperationID    string
}

type ReleaseResult struct {
	Count  int
	Amount int64
}

type ReclaimResult struct {
	Count        int
	Amount       int64
	OwnerAmounts map[int]int64
}

type reclaimBatchOwnerAmount struct {
	OwnerUserID int   `gorm:"column:owner_user_id"`
	Count       int   `gorm:"column:count"`
	Amount      int64 `gorm:"column:amount"`
}

var (
	releaseHook       ReleaseHook
	reclaimHook       ReclaimHook
	forfeitHook       ForfeitHook
	workerOnce        sync.Once
	reclaimWorkerOnce sync.Once
)

const (
	reclaimTaskPending   = "pending"
	reclaimTaskRunning   = "running"
	reclaimTaskCompleted = "completed"
	reclaimTaskFailed    = "failed"
	reclaimBatchSize     = 5000
	reclaimAdvisoryClass = int32(0x52434c4d) // RCLM
)

func RegisterReleaseHook(hook ReleaseHook) { releaseHook = hook }
func RegisterReclaimHook(hook ReclaimHook) { reclaimHook = hook }
func RegisterForfeitHook(hook ForfeitHook) { forfeitHook = hook }

func Record(params RecordParams) error {
	if params.RequestID == "" || params.GroupID == "" || params.OwnerUserID <= 0 || params.SettlementGrossAmount <= 0 {
		return nil
	}
	commission := percentage(params.SettlementGrossAmount, 5)
	fee := int64(0)
	ownerNet := params.SettlementGrossAmount - commission
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "marketplace_owner_pending", OwnerType: "user", OwnerID: int64(params.OwnerUserID), QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		platformAccount, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
			AccountType: "marketplace_platform_revenue", OwnerType: "system", OwnerID: 1, QuotaUnit: "quota",
		})
		if err != nil {
			return err
		}
		settlement := marketplaceschema.Settlement{
			RequestID: params.RequestID, GroupID: params.GroupID, OwnerUserID: params.OwnerUserID,
			ConsumerUserID: params.ConsumerUserID, BillingSource: params.BillingSource,
			ConsumerAmount: params.ConsumerDebitAmount, SettlementGrossAmount: params.SettlementGrossAmount,
			PlatformCommission: commission, TransactionFee: fee, OwnerNetAmount: ownerNet,
			Multiplier: params.WalletMultiplier, SubscriptionMultiplier: params.SubscriptionMultiplier,
			Status: statusPending, PendingAccountID: account.AccountID,
			AvailableAt: time.Now().UTC().Add(24 * time.Hour),
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "request_id"}}, DoNothing: true}).Create(&settlement)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID: account.AccountID, Amount: ownerNet, IdempotencyKey: "marketplace-pending:" + params.RequestID,
			ReasonCode: "marketplace_owner_pending", ReferenceType: "marketplace_settlement", ReferenceID: settlement.ID,
			OperatorType: "system", OperatorID: "marketplace",
		})
		if err != nil {
			return err
		}
		if commission <= 0 {
			return nil
		}
		_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID: platformAccount.AccountID, Amount: commission,
			IdempotencyKey: "marketplace-platform:" + params.RequestID,
			ReasonCode:     "marketplace_platform_revenue", ReferenceType: "marketplace_settlement", ReferenceID: settlement.ID,
			OperatorType: "system", OperatorID: "marketplace",
		})
		return err
	})
}

func StartReleaseWorker(ctx context.Context) {
	workerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				if err := ReleaseDue(200); err != nil {
					platformobservability.SysError("release marketplace settlement: " + err.Error())
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

// StartReclaimWorker processes administrator income-reclaim tasks in bounded
// transactions. A single worker per process keeps work fair; row locks on the
// task itself keep multiple control-plane processes from processing a task at
// the same time.
func StartReclaimWorker(ctx context.Context) {
	reclaimWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				processed, err := processNextIncomeReclaimTask()
				if err != nil {
					platformobservability.SysError("process marketplace income reclaim: " + err.Error())
				}
				if processed {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func ReleaseDue(limit int) error {
	if releaseHook == nil {
		return errors.New("marketplace settlement release hook is not registered")
	}
	if limit <= 0 {
		limit = 100
	}
	var settlements []marketplaceschema.Settlement
	if err := platformdb.DB.Where("status = ? AND available_at <= ?", statusPending, time.Now().UTC()).Order("available_at asc").Limit(limit).Find(&settlements).Error; err != nil {
		return err
	}
	for index := range settlements {
		if err := releaseOne(settlements[index].ID); err != nil {
			return err
		}
	}
	return nil
}

// ReleasePending releases pending owner earnings selected by an administrator.
// The normal worker only releases records after AvailableAt; this explicit path
// intentionally allows a reviewed time range or owner selection to be released
// immediately while keeping the same idempotent ledger transaction.
func ReleasePending(filter ReleaseFilter) (ReleaseResult, error) {
	if releaseHook == nil {
		return ReleaseResult{}, errors.New("marketplace settlement release hook is not registered")
	}
	if filter.Limit <= 0 {
		filter.Limit = 5000
	}
	query := platformdb.DB.Where("status = ?", statusPending).Order("created_at asc").Limit(filter.Limit)
	if len(filter.OwnerUserIDs) > 0 {
		query = query.Where("owner_user_id IN ?", filter.OwnerUserIDs)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", time.Unix(filter.StartTimestamp, 0))
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at < ?", time.Unix(filter.EndTimestamp+1, 0))
	}
	var settlements []marketplaceschema.Settlement
	if err := query.Find(&settlements).Error; err != nil {
		return ReleaseResult{}, err
	}
	result := ReleaseResult{}
	for index := range settlements {
		if err := releaseOne(settlements[index].ID); err != nil {
			return result, err
		}
		result.Count++
		result.Amount += settlements[index].OwnerNetAmount
	}
	return result, nil
}

// CreateIncomeReclaimTask creates or resumes an idempotent reclaim task. It
// deliberately does not process settlements in the HTTP request transaction.
func CreateIncomeReclaimTask(filter ReleaseFilter) (marketplaceschema.IncomeReclaim, error) {
	if filter.MaxAmount < 0 || len(filter.OperationID) > 64 {
		return marketplaceschema.IncomeReclaim{}, errors.New("invalid income reclaim amount or operation ID")
	}
	if reclaimHook == nil {
		return marketplaceschema.IncomeReclaim{}, errors.New("marketplace settlement reclaim hook is not registered")
	}
	operationID := filter.OperationID
	if operationID == "" {
		operationID = platformruntime.GetUUID()
	}
	filter.OwnerUserIDs = slices.Clone(filter.OwnerUserIDs)
	slices.Sort(filter.OwnerUserIDs)
	filter.OwnerUserIDs = slices.Compact(filter.OwnerUserIDs)
	filter.OperationID = ""
	filter.Limit = 0
	payload, err := json.Marshal(filter)
	if err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	operation := marketplaceschema.IncomeReclaim{
		ID: operationID, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(payload)),
		Filter: string(payload), Status: reclaimTaskPending,
	}
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&operation)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			var existing marketplaceschema.IncomeReclaim
			if err := tx.First(&existing, "id = ?", operationID).Error; err != nil {
				return err
			}
			if existing.Fingerprint != operation.Fingerprint {
				return errors.New("回收操作标识已用于其他筛选条件或金额")
			}
			if existing.Status == reclaimTaskFailed {
				if err := tx.Model(&existing).Updates(map[string]any{"status": reclaimTaskPending, "error_message": ""}).Error; err != nil {
					return err
				}
				existing.Status = reclaimTaskPending
				existing.ErrorMessage = ""
			}
			operation = existing
			return nil
		}
		return nil
	})
	if err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	return operation, nil
}

func GetIncomeReclaimTask(operationID string) (marketplaceschema.IncomeReclaim, error) {
	var task marketplaceschema.IncomeReclaim
	if err := platformdb.DB.First(&task, "id = ?", operationID).Error; err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	return task, nil
}

// ReclaimPending remains for command-line and legacy callers. Unlike the old
// implementation, every pass is a separate short transaction.
func ReclaimPending(filter ReleaseFilter) (ReclaimResult, error) {
	task, err := CreateIncomeReclaimTask(filter)
	if err != nil {
		return ReclaimResult{}, err
	}
	for task.Status == reclaimTaskPending || task.Status == reclaimTaskRunning {
		task, err = ProcessIncomeReclaimTask(task.ID)
		if err != nil {
			return ReclaimResult{}, err
		}
	}
	if task.Status == reclaimTaskFailed {
		return ReclaimResult{}, errors.New(task.ErrorMessage)
	}
	ownerAmounts := map[int]int64{}
	if task.OwnerAmounts != "" {
		if err := json.Unmarshal([]byte(task.OwnerAmounts), &ownerAmounts); err != nil {
			return ReclaimResult{}, err
		}
	}
	return ReclaimResult{Count: task.Count, Amount: task.Amount, OwnerAmounts: ownerAmounts}, nil
}

func processNextIncomeReclaimTask() (bool, error) {
	var task marketplaceschema.IncomeReclaim
	query := platformdb.DB.Where("status IN ?", []string{reclaimTaskPending, reclaimTaskRunning}).Order("updated_at ASC").Limit(1)
	if err := query.First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	_, err := ProcessIncomeReclaimTask(task.ID)
	return err == nil, err
}

// ProcessIncomeReclaimTask commits at most reclaimBatchSize settlements and
// their wallet transfers. The task row is locked as the cross-process mutex.
func ProcessIncomeReclaimTask(operationID string) (marketplaceschema.IncomeReclaim, error) {
	var result marketplaceschema.IncomeReclaim
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		lock := clause.Locking{Strength: "UPDATE"}
		var task marketplaceschema.IncomeReclaim
		if err := tx.Clauses(lock).First(&task, "id = ?", operationID).Error; err != nil {
			return err
		}
		if task.Status == reclaimTaskCompleted || task.Status == reclaimTaskFailed {
			result = task
			return nil
		}
		var filter ReleaseFilter
		if err := json.Unmarshal([]byte(task.Filter), &filter); err != nil {
			return err
		}
		if err := lockReclaimOwnersTx(tx, filter.OwnerUserIDs); err != nil {
			return err
		}
		if task.BatchNumber == 0 && filter.MaxAmount > 0 {
			available, err := reclaimableAmountTx(tx, filter)
			if err != nil {
				return err
			}
			if available < filter.MaxAmount {
				if err := tx.Model(&task).Updates(map[string]any{"status": reclaimTaskFailed, "error_message": "所选范围的可回收收益不足，未扣除额度，请刷新后重试"}).Error; err != nil {
					return err
				}
				task.Status = reclaimTaskFailed
				task.ErrorMessage = "所选范围的可回收收益不足，未扣除额度，请刷新后重试"
				result = task
				return nil
			}
		}
		ownerAmounts := make(map[int]int64)
		batchAmount, batchCount := int64(0), 0
		if platformdb.UsingPostgreSQL {
			remaining := int64(0)
			if filter.MaxAmount > 0 {
				remaining = filter.MaxAmount - task.Amount
			}
			amounts, err := reclaimPostgresBatchTx(tx, filter, remaining, time.Now().UTC())
			if err != nil {
				return err
			}
			for _, item := range amounts {
				ownerAmounts[item.OwnerUserID] = item.Amount
				batchAmount += item.Amount
				batchCount += item.Count
			}
		} else {
			query := reclaimSettlementQuery(tx, filter).
				Select("id", "owner_user_id", "owner_net_amount", "reclaimed_amount", "created_at").
				Order("created_at ASC, id ASC").Limit(reclaimBatchSize).
				Clauses(lock)
			var items []marketplaceschema.Settlement
			if err := query.Find(&items).Error; err != nil {
				return err
			}
			var fullIDs []string
			now := time.Now().UTC()
			for _, item := range items {
				amount := item.OwnerNetAmount - item.ReclaimedAmount
				if filter.MaxAmount > 0 {
					amount = min(amount, filter.MaxAmount-task.Amount-batchAmount)
				}
				if amount <= 0 {
					break
				}
				ownerAmounts[item.OwnerUserID] += amount
				if item.ReclaimedAmount+amount == item.OwnerNetAmount {
					fullIDs = append(fullIDs, item.ID)
				} else if err := tx.Model(&item).Updates(map[string]any{"reclaimed_amount": item.ReclaimedAmount + amount, "reclaimed_at": now}).Error; err != nil {
					return err
				}
				batchCount++
				batchAmount += amount
				if filter.MaxAmount > 0 && task.Amount+batchAmount == filter.MaxAmount {
					break
				}
			}
			if len(fullIDs) > 0 {
				if err := tx.Model(&marketplaceschema.Settlement{}).Where("id IN ?", fullIDs).Updates(map[string]any{
					"reclaimed_amount": gorm.Expr("owner_net_amount"), "reclaimed_at": now, "status": statusReclaimed,
				}).Error; err != nil {
					return err
				}
			}
		}
		if batchCount == 0 {
			if err := tx.Model(&task).Update("status", reclaimTaskCompleted).Error; err != nil {
				return err
			}
			task.Status = reclaimTaskCompleted
			result = task
			return nil
		}
		owners := make([]int, 0, len(ownerAmounts))
		for owner := range ownerAmounts {
			owners = append(owners, owner)
		}
		slices.Sort(owners)
		for _, owner := range owners {
			if err := reclaimHook(tx, owner, 1, ownerAmounts[owner], fmt.Sprintf("marketplace-reclaim:%s:batch:%d:owner:%d", task.ID, task.BatchNumber+1, owner)); err != nil {
				return err
			}
		}
		cumulativeOwnerAmounts := map[int]int64{}
		if task.OwnerAmounts != "" {
			if err := json.Unmarshal([]byte(task.OwnerAmounts), &cumulativeOwnerAmounts); err != nil {
				return err
			}
		}
		for owner, amount := range ownerAmounts {
			cumulativeOwnerAmounts[owner] += amount
		}
		ownerAmountsJSON, err := json.Marshal(cumulativeOwnerAmounts)
		if err != nil {
			return err
		}
		newCount, newAmount, newBatchNumber := task.Count+batchCount, task.Amount+batchAmount, task.BatchNumber+1
		updates := map[string]any{"status": reclaimTaskRunning, "count": newCount, "amount": newAmount, "batch_number": newBatchNumber, "owner_amounts": string(ownerAmountsJSON), "error_message": ""}
		if (filter.MaxAmount > 0 && newAmount == filter.MaxAmount) || batchCount < reclaimBatchSize {
			updates["status"] = reclaimTaskCompleted
		}
		if err := tx.Model(&marketplaceschema.IncomeReclaim{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			return err
		}
		task.Count = newCount
		task.Amount = newAmount
		task.BatchNumber = newBatchNumber
		task.OwnerAmounts = string(ownerAmountsJSON)
		task.Status = updates["status"].(string)
		result = task
		return nil
	})
	if err != nil {
		// All financial mutations above have rolled back. Record the failure in a
		// separate short transaction so the same operation ID can resume later.
		_ = platformdb.DB.Model(&marketplaceschema.IncomeReclaim{}).Where("id = ?", operationID).Updates(map[string]any{"status": reclaimTaskFailed, "error_message": err.Error()}).Error
		return marketplaceschema.IncomeReclaim{}, err
	}
	return result, nil
}

// reclaimPostgresBatchTx avoids sending thousands of settlement IDs back to
// PostgreSQL in an IN list. The candidate lock, partial-amount calculation,
// update, and per-owner aggregation stay in one statement and one transaction.
func reclaimPostgresBatchTx(tx *gorm.DB, filter ReleaseFilter, remaining int64, now time.Time) ([]reclaimBatchOwnerAmount, error) {
	locked := reclaimSettlementQuery(tx, filter).
		Select("id", "owner_user_id", "owner_net_amount", "reclaimed_amount", "created_at").
		Order("created_at ASC, id ASC").Limit(reclaimBatchSize).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	table := marketplaceschema.Settlement{}.TableName()
	statement := fmt.Sprintf(`
WITH locked AS MATERIALIZED (?),
amounts AS (
	SELECT id, owner_user_id, owner_net_amount, reclaimed_amount,
		CASE WHEN CAST(? AS BIGINT) <= 0 THEN owner_net_amount - reclaimed_amount
		ELSE LEAST(
			owner_net_amount - reclaimed_amount,
			GREATEST(CAST(? AS BIGINT) - COALESCE(
				SUM(owner_net_amount - reclaimed_amount) OVER (
					ORDER BY created_at ASC, id ASC
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
				), 0
			), 0))
		END AS reclaim_amount
	FROM locked
),
updated AS (
	UPDATE %s AS settlement
	SET reclaimed_amount = settlement.reclaimed_amount + amounts.reclaim_amount,
		reclaimed_at = ?,
		status = CASE
			WHEN settlement.reclaimed_amount + amounts.reclaim_amount = settlement.owner_net_amount THEN ?
			ELSE settlement.status
		END
	FROM amounts
	WHERE settlement.id = amounts.id AND amounts.reclaim_amount > 0
	RETURNING settlement.owner_user_id, amounts.reclaim_amount
)
SELECT owner_user_id, COUNT(*) AS count, COALESCE(SUM(reclaim_amount), 0) AS amount
FROM updated
GROUP BY owner_user_id
ORDER BY owner_user_id`, table)
	var result []reclaimBatchOwnerAmount
	err := tx.Raw(statement, locked, remaining, remaining, now, statusReclaimed).Scan(&result).Error
	return result, err
}

func reclaimSettlementQuery(tx *gorm.DB, filter ReleaseFilter) *gorm.DB {
	query := tx.Session(&gorm.Session{NewDB: true}).Model(&marketplaceschema.Settlement{}).
		Where("status = ? AND owner_net_amount > reclaimed_amount", statusReleased)
	if len(filter.OwnerUserIDs) > 0 {
		query = query.Where("owner_user_id IN ?", filter.OwnerUserIDs)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", time.Unix(filter.StartTimestamp, 0))
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at < ?", time.Unix(filter.EndTimestamp+1, 0))
	}
	return query
}

func reclaimableAmountTx(tx *gorm.DB, filter ReleaseFilter) (int64, error) {
	var amount int64
	err := reclaimSettlementQuery(tx, filter).Select("COALESCE(SUM(owner_net_amount - reclaimed_amount), 0)").Scan(&amount).Error
	return amount, err
}

// Explicit-owner tasks share the global gate and lock only their owners, so
// unrelated owners can progress concurrently. An unscoped task takes the
// global gate exclusively because it can touch every owner.
func lockReclaimOwnersTx(tx *gorm.DB, ownerUserIDs []int) error {
	if !platformdb.UsingPostgreSQL {
		return nil
	}
	if len(ownerUserIDs) == 0 {
		return tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", reclaimAdvisoryClass, int32(0)).Error
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(?, ?)", reclaimAdvisoryClass, int32(0)).Error; err != nil {
		return err
	}
	for _, ownerUserID := range ownerUserIDs {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", reclaimAdvisoryClass, int32(ownerUserID)).Error; err != nil {
			return err
		}
	}
	return nil
}

// ForfeitChannelPending clears frozen pending earnings when a channel is shut down.
func ForfeitChannelPending(channelID string) (ReclaimResult, error) {
	var groupID string
	if err := platformdb.DB.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channelID).Pluck("id", &groupID).Error; err != nil {
		return ReclaimResult{}, err
	}
	var settlements []marketplaceschema.Settlement
	if err := platformdb.DB.Where("group_id = ? AND status = ?", groupID, statusPending).Find(&settlements).Error; err != nil {
		if isMissingSettlementTable(err) {
			return ReclaimResult{}, nil
		}
		return ReclaimResult{}, err
	}
	if len(settlements) == 0 {
		return ReclaimResult{}, nil
	}
	if forfeitHook == nil {
		return ReclaimResult{}, errors.New("marketplace settlement forfeit hook is not registered")
	}
	result := ReclaimResult{}
	for _, item := range settlements {
		if err := forfeitOne(item.ID); err != nil {
			return result, err
		}
		result.Count++
		result.Amount += item.OwnerNetAmount
	}
	return result, nil
}

func ForfeitChannelPendingTx(tx *gorm.DB, channelID string) (ReclaimResult, error) {
	var groupID string
	if err := tx.Model(&marketplaceschema.Group{}).Where("channel_id = ?", channelID).Pluck("id", &groupID).Error; err != nil {
		return ReclaimResult{}, err
	}
	var settlements []marketplaceschema.Settlement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("group_id = ? AND status = ?", groupID, statusPending).Find(&settlements).Error; err != nil {
		if isMissingSettlementTable(err) {
			return ReclaimResult{}, nil
		}
		return ReclaimResult{}, err
	}
	if len(settlements) == 0 {
		return ReclaimResult{}, nil
	}
	if forfeitHook == nil {
		return ReclaimResult{}, errors.New("marketplace settlement forfeit hook is not registered")
	}
	result := ReclaimResult{}
	for _, item := range settlements {
		if err := forfeitOneTx(tx, &item); err != nil {
			return result, err
		}
		result.Count++
		result.Amount += item.OwnerNetAmount
	}
	return result, nil
}

func isMissingSettlementTable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "does not exist")
}

func forfeitOne(settlementID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var item marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", settlementID).Error; err != nil {
			return err
		}
		if item.Status != statusPending {
			return nil
		}
		if err := forfeitHook(tx, item.PendingAccountID, 1, item.OwnerNetAmount, "marketplace-forfeit:"+item.ID); err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
	})
}

func forfeitOneTx(tx *gorm.DB, item *marketplaceschema.Settlement) error {
	if item.Status != statusPending {
		return nil
	}
	if err := forfeitHook(tx, item.PendingAccountID, 1, item.OwnerNetAmount, "marketplace-forfeit:"+item.ID); err != nil {
		return err
	}
	now := time.Now().UTC()
	return tx.Model(item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
}

func releaseOne(settlementID string) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var item marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", settlementID).Error; err != nil {
			return err
		}
		if item.Status == statusReleased {
			return nil
		}
		reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
			AccountID: item.PendingAccountID, RequestID: "marketplace-release:" + item.ID,
			ReservedAmount: item.OwnerNetAmount, IdempotencyKey: "marketplace-release-reserve:" + item.ID,
		})
		if err != nil {
			return err
		}
		if _, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
			ReservationID: reservation.ReservationID, ActualAmount: item.OwnerNetAmount,
			IdempotencyKey: "marketplace-release-settle:" + item.ID,
		}); err != nil {
			return err
		}
		if err := releaseHook(tx, item.OwnerUserID, item.OwnerNetAmount, "marketplace-release-credit:"+item.ID, "marketplace_owner_release"); err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&item).Updates(map[string]any{"status": statusReleased, "released_at": now}).Error
	})
}

func percentage(amount int64, percent int64) int64 {
	return (amount*percent + 50) / 100
}
