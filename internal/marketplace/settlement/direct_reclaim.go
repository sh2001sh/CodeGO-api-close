package settlement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	reclaimTaskDebitPending = "debit_pending"
	reclaimTaskCharged      = "charged"
)

// CreateChargedIncomeReclaimTaskTx records a wallet transfer committed from a
// reservation. It deliberately avoids the settlement-history advisory lock:
// the direct worker acquires that lock later while materializing history, so a
// slow audit can never delay the administrator's payout confirmation.
func CreateChargedIncomeReclaimTaskTx(tx *gorm.DB, ownerUserID int, amount int64, operationID string) (marketplaceschema.IncomeReclaim, error) {
	if tx == nil || ownerUserID <= 0 || amount <= 0 || operationID == "" || len(operationID) > 64 {
		return marketplaceschema.IncomeReclaim{}, errors.New("invalid reserved income reclaim task")
	}
	owners := []int{ownerUserID}
	amounts := map[int]int64{ownerUserID: amount}
	filter := ReleaseFilter{
		OwnerUserIDs: owners, MaxAmount: amount, Direct: true,
		AuditRemaining: map[int]int64{ownerUserID: amount},
	}
	payload, err := json.Marshal(filter)
	if err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	ownerAmounts, err := json.Marshal(amounts)
	if err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	task := marketplaceschema.IncomeReclaim{
		ID: operationID, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(payload)),
		Filter: string(payload), Status: reclaimTaskCharged, Amount: amount,
		OwnerAmounts: string(ownerAmounts),
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&task)
	if result.Error != nil {
		return marketplaceschema.IncomeReclaim{}, result.Error
	}
	if result.RowsAffected == 1 {
		return task, nil
	}
	var existing marketplaceschema.IncomeReclaim
	if err := tx.First(&existing, "id = ?", operationID).Error; err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	if existing.Fingerprint != task.Fingerprint || existing.Amount != amount || existing.OwnerAmounts != task.OwnerAmounts {
		return marketplaceschema.IncomeReclaim{}, errors.New("回收操作标识已用于其他渠道主或金额")
	}
	return existing, nil
}

// LockOwnerReclaimsTx serializes wallet reservations with direct reclaims for
// the same owner, preventing both paths from claiming the same released income.
func LockOwnerReclaimsTx(tx *gorm.DB, ownerUserIDs []int) error {
	return lockReclaimOwnersTx(tx, ownerUserIDs)
}

var directReclaimOnce sync.Once

// Direct tasks use distinct statuses so older ledger workers cannot debit them.
// The control plane owns the debit and resumable, bounded audit work.
func StartDirectReclaimWorker(ctx context.Context) {
	directReclaimOnce.Do(func() {
		go func() {
			for ctx.Err() == nil {
				var task marketplaceschema.IncomeReclaim
				err := platformdb.DB.WithContext(ctx).Where("status IN ?", []string{reclaimTaskDebitPending, reclaimTaskCharged}).Order("updated_at ASC, id ASC").First(&task).Error
				if err == nil {
					_, err = ProcessDirectIncomeReclaimTask(task.ID)
				}
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					platformobservability.SysError("process direct income reclaim: " + err.Error())
				}
				if err == nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}()
	})
}

// Until the audit has caught up, another operation for the same owner must not
// reclaim the same history. The caller holds the existing per-owner mutex.
func checkOutstandingDirectReclaimsTx(tx *gorm.DB, operationID string, owners []int) error {
	var tasks []marketplaceschema.IncomeReclaim
	if err := tx.Where("id <> ? AND status IN ?", operationID, []string{reclaimTaskDebitPending, reclaimTaskCharged, reclaimTaskPending, reclaimTaskRunning}).Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		var filter ReleaseFilter
		if err := json.Unmarshal([]byte(task.Filter), &filter); err != nil {
			return err
		}
		for _, owner := range owners {
			if len(filter.OwnerUserIDs) == 0 || slices.Contains(filter.OwnerUserIDs, owner) {
				return errors.New("所选渠道主上一笔回收仍在处理或补齐明细，请等待该笔完成，勿重复提交")
			}
		}
	}
	return nil
}

// ProcessDirectIncomeReclaimTask commits native wallet debits and their durable
// receipt first. Later calls ONLY materialize history and never touch wallets.
func ProcessDirectIncomeReclaimTask(operationID string) (marketplaceschema.IncomeReclaim, error) {
	var task marketplaceschema.IncomeReclaim
	initialStatus := ""
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", operationID).Error; err != nil {
			return err
		}
		initialStatus = task.Status
		if task.Status != reclaimTaskDebitPending && task.Status != reclaimTaskCharged {
			return nil
		}
		var filter ReleaseFilter
		if err := json.Unmarshal([]byte(task.Filter), &filter); err != nil {
			return err
		}
		if !filter.Direct || len(filter.OwnerUserIDs) == 0 {
			return errors.New("invalid direct reclaim receipt")
		}
		if err := lockReclaimOwnersTx(tx, filter.OwnerUserIDs); err != nil {
			return err
		}
		if task.Status == reclaimTaskDebitPending {
			return chargeDirectReclaimTx(tx, &task, &filter)
		}
		return auditDirectReclaimTx(tx, &task, &filter)
	})
	if err != nil {
		// A failed audit retains charged status: funds were already transferred.
		status := reclaimTaskFailed
		if initialStatus == reclaimTaskCharged {
			status = reclaimTaskCharged
		}
		if recordErr := platformdb.DB.Model(&marketplaceschema.IncomeReclaim{}).Where("id = ? AND status = ?", operationID, initialStatus).Updates(map[string]any{"status": status, "error_message": err.Error()}).Error; recordErr != nil {
			return task, errors.Join(err, recordErr)
		}
	}
	if err == nil && initialStatus == reclaimTaskDebitPending {
		var amounts map[int]int64
		if decodeErr := json.Unmarshal([]byte(task.OwnerAmounts), &amounts); decodeErr != nil {
			platformobservability.SysError("decode committed direct reclaim for cache invalidation: " + decodeErr.Error())
		}
		owners := []int{1}
		for owner := range amounts {
			owners = append(owners, owner)
		}
		for _, owner := range owners {
			if cacheErr := platformcache.DeleteUserCache(owner); cacheErr != nil {
				platformobservability.SysError(fmt.Sprintf("direct reclaim committed; user %d cache invalidation failed: %v", owner, cacheErr))
			}
		}
	}
	return task, err
}

func chargeDirectReclaimTx(tx *gorm.DB, task *marketplaceschema.IncomeReclaim, filter *ReleaseFilter) error {
	if reclaimHook == nil || reclaimBalancesHook == nil {
		return errors.New("direct reclaim wallet hooks are not registered")
	}
	// Read aggregates once; no historical row updates precede the financial commit.
	var totals []reclaimBatchOwnerAmount
	if err := reclaimSettlementQuery(tx, *filter).Select("owner_user_id, COUNT(*) AS count, SUM(owner_net_amount - reclaimed_amount) AS amount").Group("owner_user_id").Order("owner_user_id ASC").Scan(&totals).Error; err != nil {
		return err
	}
	balances, err := reclaimBalancesHook(tx, filter.OwnerUserIDs)
	if err != nil {
		return err
	}
	heldByOwner := map[int]int64{}
	if tx.Migrator().HasTable(&marketplaceschema.OwnerWithdrawal{}) {
		var held []reclaimBatchOwnerAmount
		if err := tx.Model(&marketplaceschema.OwnerWithdrawal{}).
			Where("owner_user_id IN ? AND reservation_id <> ? AND status IN ?", filter.OwnerUserIDs, "", []string{
				marketplaceschema.OwnerWithdrawalStatusPendingTransfer,
				marketplaceschema.OwnerWithdrawalStatusNeedsUpdate,
			}).
			Select("owner_user_id, SUM(requested_quota) AS amount").Group("owner_user_id").Scan(&held).Error; err != nil {
			return err
		}
		for _, item := range held {
			heldByOwner[item.OwnerUserID] = item.Amount
		}
	}
	available := int64(0)
	for i := range totals {
		totals[i].Amount = max(int64(0), totals[i].Amount-heldByOwner[totals[i].OwnerUserID])
		totals[i].Amount = min(totals[i].Amount, max(int64(0), balances[totals[i].OwnerUserID]))
		available += totals[i].Amount
	}
	if filter.MaxAmount > available {
		return errors.New("所选范围的可回收收益或可用余额不足，未扣除额度，请刷新后重试")
	}
	remaining := available
	if filter.MaxAmount > 0 {
		remaining = filter.MaxAmount
	}
	amounts := map[int]int64{}
	for _, total := range totals {
		amount := min(total.Amount, remaining)
		if amount <= 0 {
			continue
		}
		key := fmt.Sprintf("marketplace-reclaim:%s:direct:owner:%d", task.ID, total.OwnerUserID)
		if err := reclaimHook(tx, total.OwnerUserID, 1, amount, key); err != nil {
			return err
		}
		amounts[total.OwnerUserID] = amount
		task.Amount += amount
		remaining -= amount
	}
	filter.AuditRemaining = amounts
	encoded, err := json.Marshal(amounts)
	if err != nil {
		return err
	}
	task.OwnerAmounts = string(encoded)
	task.Status = reclaimTaskCharged
	if task.Amount == 0 {
		task.Status = reclaimTaskCompleted
	}
	return saveDirectReclaimTx(tx, task, filter)
}

func auditDirectReclaimTx(tx *gorm.DB, task *marketplaceschema.IncomeReclaim, filter *ReleaseFilter) error {
	owners := make([]int, 0, len(filter.AuditRemaining))
	for owner, remaining := range filter.AuditRemaining {
		if remaining > 0 {
			owners = append(owners, owner)
		}
	}
	slices.Sort(owners)
	if len(owners) == 0 {
		task.Status = reclaimTaskCompleted
		return saveDirectReclaimTx(tx, task, filter)
	}
	owner := owners[0]
	remaining := filter.AuditRemaining[owner]
	scope := *filter
	scope.OwnerUserIDs = []int{owner}
	amount, count := int64(0), 0
	if platformdb.UsingPostgreSQL {
		rows, err := reclaimPostgresTx(tx, scope, remaining, time.Now().UTC(), time.Time{}, "")
		if err != nil {
			return err
		}
		for _, row := range rows {
			amount += row.Amount
			count += row.Count
		}
	} else {
		var rows []marketplaceschema.Settlement
		if err := reclaimSettlementQuery(tx, scope).Order("created_at ASC, id ASC").Limit(reclaimBatchSize).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			quota := min(row.OwnerNetAmount-row.ReclaimedAmount, remaining-amount)
			if quota <= 0 {
				break
			}
			status := statusReleased
			if row.ReclaimedAmount+quota == row.OwnerNetAmount {
				status = statusReclaimed
			}
			if err := tx.Model(&row).Updates(map[string]any{"reclaimed_amount": row.ReclaimedAmount + quota, "reclaimed_at": time.Now().UTC(), "status": status}).Error; err != nil {
				return err
			}
			amount += quota
			count++
		}
	}
	if amount <= 0 || amount > remaining {
		return errors.New("已扣款回收的明细金额不匹配，保留回执等待重试，禁止再次扣款")
	}
	filter.AuditRemaining[owner] -= amount
	if filter.AuditRemaining[owner] == 0 {
		delete(filter.AuditRemaining, owner)
	}
	task.Count += count
	task.BatchNumber++
	if len(filter.AuditRemaining) == 0 {
		task.Status = reclaimTaskCompleted
	}
	return saveDirectReclaimTx(tx, task, filter)
}

func saveDirectReclaimTx(tx *gorm.DB, task *marketplaceschema.IncomeReclaim, filter *ReleaseFilter) error {
	encoded, err := json.Marshal(filter)
	if err != nil {
		return err
	}
	task.Filter = string(encoded)
	task.ErrorMessage = ""
	return tx.Model(task).Updates(map[string]any{"filter": task.Filter, "status": task.Status, "count": task.Count, "amount": task.Amount, "owner_amounts": task.OwnerAmounts, "batch_number": task.BatchNumber, "error_message": ""}).Error
}
