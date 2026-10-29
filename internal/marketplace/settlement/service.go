package settlement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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

	releaseBatchSize              = 1000
	releaseConcurrency            = 12
	releaseOwnerSelectionSize     = releaseBatchSize
	releaseOwnerTransferBatchSize = 250
	releaseMaxAttempts            = 3
	releaseIdleDelay              = 5 * time.Second
	releaseErrorDelay             = time.Second
	releaseRetryDelay             = 25 * time.Millisecond
)

type RecordParams struct {
	RequestID                 string
	GroupID                   string
	OwnerUserID               int
	ConsumerUserID            int
	BillingSource             string
	ConsumerDebitAmount       int64
	SettlementGrossAmount     int64
	WalletMultiplier          float64
	SubscriptionMultiplier    float64
	CreditPackGrantedQuota    int64
	CreditPackPaidQuota       int64
	CreditPackChannelShareBPS int64
	FeePolicyVersion          string
	SubscriptionID            int
	SubscriptionPlanID        int
	SubscriptionPlanTitle     string
}

// An empty version belongs to already queued historical settlement contexts.
const FeePolicyPaidBonusV2 = "paid_bonus_rate_v2"

type IncomeSnapshot struct {
	AlgorithmVersion      string  `json:"algorithm_version"`
	BillingSource         string  `json:"billing_source"`
	SubscriptionID        int     `json:"subscription_id"`
	SubscriptionPlanID    int     `json:"subscription_plan_id"`
	SubscriptionPlanTitle string  `json:"subscription_plan_title"`
	BaseCommissionRateBPS int64   `json:"base_commission_rate_bps"`
	PackageBonusRate      float64 `json:"package_bonus_rate"`
	ChannelShareBPS       int64   `json:"channel_share_bps"`
	PackageSurchargeRate  float64 `json:"package_surcharge_rate"`
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
	Direct         bool          `json:",omitempty"`
	AuditRemaining map[int]int64 `json:",omitempty"`
}

type ReleaseResult struct {
	Count  int
	Amount int64
}

type releaseOwnerBatch struct {
	ownerUserID    int
	accountBatches []releaseAccountBatch
}

type releaseAccountBatch struct {
	pendingAccountID string
	settlementIDs    []string
}

type ReclaimResult struct {
	Count        int
	Amount       int64
	OwnerAmounts map[int]int64
}

type reclaimBatchOwnerAmount struct {
	OwnerUserID     int       `gorm:"column:owner_user_id"`
	Count           int       `gorm:"column:count"`
	Amount          int64     `gorm:"column:amount"`
	CursorCreatedAt time.Time `gorm:"column:cursor_created_at"`
	CursorID        string    `gorm:"column:cursor_id"`
}

var (
	releaseHook         ReleaseHook
	reclaimHook         ReclaimHook
	reclaimBalancesHook func(*gorm.DB, []int) (map[int]int64, error)
	forfeitHook         ForfeitHook
	workerOnce          sync.Once
	reclaimWorkerOnce   sync.Once
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
func RegisterReclaimBalancesHook(hook func(*gorm.DB, []int) (map[int]int64, error)) {
	reclaimBalancesHook = hook
}
func RegisterForfeitHook(hook ForfeitHook) { forfeitHook = hook }

func Record(params RecordParams) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error { return RecordTx(tx, params) })
}

// RecordTx lets durable relay settlement processing create late marketplace
// records in the same transaction as their final referral allocation.
func RecordTx(tx *gorm.DB, params RecordParams) error {
	if params.RequestID == "" || params.GroupID == "" || params.OwnerUserID <= 0 || params.SettlementGrossAmount <= 0 {
		return nil
	}
	if tx == nil {
		return errors.New("marketplace settlement transaction is required")
	}
	var existing marketplaceschema.Settlement
	if err := tx.Where("request_id = ?", params.RequestID).First(&existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	commission := percentage(params.SettlementGrossAmount, 5)
	discount, fee, err := creditPackCharges(params)
	if err != nil {
		return err
	}
	if fee > params.SettlementGrossAmount-commission {
		return errors.New("credit pack fees exceed settlement gross amount")
	}
	ownerNet := params.SettlementGrossAmount - commission - fee
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
		CreditPackGrantedQuota: params.CreditPackGrantedQuota, CreditPackPaidQuota: params.CreditPackPaidQuota,
		CreditPackChannelShareBPS: params.CreditPackChannelShareBPS,
		CreditPackDiscountAmount:  discount, CreditPackPlatformSubsidyAmount: max(discount-fee, 0),
		ReferralRebatePending: params.CreditPackGrantedQuota > 0,
		Status:                statusPending, PendingAccountID: account.AccountID,
		AvailableAt: time.Now().UTC().Add(24 * time.Hour),
	}
	result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "request_id"}}, DoNothing: true}).Create(&settlement)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	entry, err := billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: ownerNet, IdempotencyKey: "marketplace-pending:" + params.RequestID,
		ReasonCode: "marketplace_owner_pending", ReferenceType: "marketplace_settlement", ReferenceID: settlement.ID,
		OperatorType: "system", OperatorID: "marketplace",
	})
	if err != nil {
		return err
	}
	snapshot := IncomeSnapshot{
		AlgorithmVersion: params.FeePolicyVersion, BillingSource: params.BillingSource,
		SubscriptionID: params.SubscriptionID, SubscriptionPlanID: params.SubscriptionPlanID,
		SubscriptionPlanTitle: params.SubscriptionPlanTitle, BaseCommissionRateBPS: 500,
		ChannelShareBPS: params.CreditPackChannelShareBPS,
	}
	if snapshot.AlgorithmVersion == "" {
		snapshot.AlgorithmVersion = "granted_bonus_share_v1"
	}
	if params.CreditPackPaidQuota > 0 {
		snapshot.PackageBonusRate = float64(params.CreditPackGrantedQuota-params.CreditPackPaidQuota) / float64(params.CreditPackPaidQuota)
		denominator := params.CreditPackGrantedQuota
		if params.FeePolicyVersion == FeePolicyPaidBonusV2 {
			denominator = params.CreditPackPaidQuota
		}
		snapshot.PackageSurchargeRate = float64(params.CreditPackGrantedQuota-params.CreditPackPaidQuota) / float64(denominator) * float64(params.CreditPackChannelShareBPS) / 10000
	}
	metadata, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := tx.Model(entry).Update("metadata", metadata).Error; err != nil {
		return err
	}
	if commission+fee <= 0 {
		return nil
	}
	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID: platformAccount.AccountID, Amount: commission + fee,
		IdempotencyKey: "marketplace-platform:" + params.RequestID,
		ReasonCode:     "marketplace_platform_revenue", ReferenceType: "marketplace_settlement", ReferenceID: settlement.ID,
		OperatorType: "system", OperatorID: "marketplace",
	})
	return err
}

func StartReleaseWorker(ctx context.Context) {
	workerOnce.Do(func() {
		go func() {
			for {
				selected, err := releaseDue(ctx, releaseBatchSize, releaseConcurrency)
				if err != nil {
					platformobservability.SysError("release marketplace settlement: " + err.Error())
				}
				compacted, compactErr := CompactIncomeProjection(ctx)
				if compactErr != nil {
					platformobservability.SysError("compact marketplace income projection: " + compactErr.Error())
				}

				delay := time.Duration(0)
				switch {
				case err != nil || compactErr != nil:
					delay = releaseErrorDelay
				case selected == 0 && compacted < 10000:
					delay = releaseIdleDelay
				}
				if delay == 0 {
					continue
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return
				case <-timer.C:
				}
			}
		}()
	})
}

// StartReclaimWorker processes administrator income-reclaim tasks outside HTTP
// requests. A single worker per process keeps work fair; row locks on the
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
	_, err := releaseDue(context.Background(), limit, releaseConcurrency)
	return err
}

func releaseDue(ctx context.Context, limit int, concurrency int) (int, error) {
	if releaseHook == nil {
		return 0, errors.New("marketplace settlement release hook is not registered")
	}
	if limit <= 0 {
		limit = 100
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	// SQLite's in-memory test database is connection-local and does not support
	// concurrent writers. Production PostgreSQL keeps the bounded concurrency.
	if !platformdb.UsingPostgreSQL {
		concurrency = 1
	}
	settlements, err := selectDueSettlements(ctx, limit, releaseOwnerSelectionSize)
	if err != nil {
		return 0, err
	}
	if len(settlements) == 0 {
		return 0, nil
	}
	ownerBatches := groupSettlementReleasesByOwner(settlements)
	concurrency = min(concurrency, len(ownerBatches))
	jobs := make(chan releaseOwnerBatch)
	var workers sync.WaitGroup
	var errorMu sync.Mutex
	var firstError error
	errorCount := 0
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for batch := range jobs {
				// Every release for one owner writes the same mirrored wallet row.
				// Keep that lock chain serial while different owners run in parallel.
				for _, accountBatch := range batch.accountBatches {
					if platformdb.UsingPostgreSQL && platformdb.DB.Dialector.Name() == "postgres" {
						processed, err := releaseOwnerAccountBatchContext(ctx, batch.ownerUserID, accountBatch)
						if err == nil {
							continue
						}
						errorMu.Lock()
						errorCount += len(accountBatch.settlementIDs) - processed
						if firstError == nil {
							firstError = fmt.Errorf("owner %d pending account %s: %w", batch.ownerUserID, accountBatch.pendingAccountID, err)
						}
						errorMu.Unlock()
						continue
					}
					for _, settlementID := range accountBatch.settlementIDs {
						if err := releaseOneContext(ctx, settlementID); err != nil {
							errorMu.Lock()
							errorCount++
							if firstError == nil {
								firstError = fmt.Errorf("settlement %s: %w", settlementID, err)
							}
							errorMu.Unlock()
						}
					}
				}
			}
		}()
	}
	for index := range ownerBatches {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return len(settlements), ctx.Err()
		case jobs <- ownerBatches[index]:
		}
	}
	close(jobs)
	workers.Wait()
	if firstError != nil {
		return len(settlements), fmt.Errorf("%d of %d marketplace settlements failed to release; first error: %w", errorCount, len(settlements), firstError)
	}
	return len(settlements), nil
}

func selectDueSettlements(ctx context.Context, limit int, ownerLimit int) ([]marketplaceschema.Settlement, error) {
	if limit <= 0 {
		return nil, nil
	}
	ownerLimit = min(max(ownerLimit, 1), limit)
	now := time.Now().UTC()
	type dueOwner struct {
		OwnerUserID int `gorm:"column:owner_user_id"`
	}
	var owners []dueOwner
	if err := platformdb.DB.WithContext(ctx).Model(&marketplaceschema.Settlement{}).
		Select("owner_user_id").
		Where("status = ? AND available_at <= ?", statusPending, now).
		Where("referral_rebate_pending = ?", false).
		Group("owner_user_id").Order("MIN(available_at) asc, owner_user_id asc").Limit(ownerLimit).
		Find(&owners).Error; err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		return nil, nil
	}
	ownerIDs := make([]int, len(owners))
	for index := range owners {
		ownerIDs[index] = owners[index].OwnerUserID
	}
	perOwner := (limit + len(ownerIDs) - 1) / len(ownerIDs)
	ranked := platformdb.DB.WithContext(ctx).Model(&marketplaceschema.Settlement{}).
		Select("id, owner_user_id, pending_account_id, available_at, ROW_NUMBER() OVER (PARTITION BY owner_user_id ORDER BY available_at ASC, id ASC) AS owner_rank").
		Where("status = ? AND available_at <= ?", statusPending, now).
		Where("referral_rebate_pending = ? AND owner_user_id IN ?", false, ownerIDs)
	var settlements []marketplaceschema.Settlement
	err := platformdb.DB.WithContext(ctx).Table("(?) AS ranked_due_settlements", ranked).
		Select("id, owner_user_id, pending_account_id, available_at").
		Where("owner_rank <= ?", perOwner).
		Order("available_at asc, id asc").Limit(limit).Find(&settlements).Error
	return settlements, err
}

func groupSettlementReleasesByOwner(settlements []marketplaceschema.Settlement) []releaseOwnerBatch {
	batchIndexByOwner := make(map[int]int, len(settlements))
	accountIndexByOwner := make(map[int]map[string]int, len(settlements))
	batches := make([]releaseOwnerBatch, 0, len(settlements))
	for _, item := range settlements {
		ownerIndex, found := batchIndexByOwner[item.OwnerUserID]
		if !found {
			ownerIndex = len(batches)
			batchIndexByOwner[item.OwnerUserID] = ownerIndex
			accountIndexByOwner[item.OwnerUserID] = make(map[string]int)
			batches = append(batches, releaseOwnerBatch{ownerUserID: item.OwnerUserID})
		}
		accountIndexes := accountIndexByOwner[item.OwnerUserID]
		accountIndex, found := accountIndexes[item.PendingAccountID]
		if !found {
			accountIndex = len(batches[ownerIndex].accountBatches)
			accountIndexes[item.PendingAccountID] = accountIndex
			batches[ownerIndex].accountBatches = append(batches[ownerIndex].accountBatches, releaseAccountBatch{pendingAccountID: item.PendingAccountID})
		}
		batches[ownerIndex].accountBatches[accountIndex].settlementIDs = append(
			batches[ownerIndex].accountBatches[accountIndex].settlementIDs, item.ID,
		)
	}
	return batches
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
	filter.AuditRemaining = nil
	payload, err := json.Marshal(filter)
	if err != nil {
		return marketplaceschema.IncomeReclaim{}, err
	}
	operation := marketplaceschema.IncomeReclaim{
		ID: operationID, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(payload)),
		Filter: string(payload), Status: reclaimTaskPending,
	}
	if filter.Direct {
		operation.Status = reclaimTaskDebitPending
	}
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if filter.Direct {
			if len(filter.OwnerUserIDs) == 0 || filter.OwnerUserIDs[0] <= 0 || reclaimBalancesHook == nil {
				return errors.New("直扣需要明确的渠道主及原生钱包余额读取")
			}
			if err := lockReclaimOwnersTx(tx, filter.OwnerUserIDs); err != nil {
				return err
			}
		}
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
				if filter.Direct {
					if err := checkOutstandingDirectReclaimsTx(tx, operationID, filter.OwnerUserIDs); err != nil {
						return err
					}
				}
				nextStatus := reclaimTaskPending
				if filter.Direct {
					nextStatus = reclaimTaskDebitPending
				}
				if err := tx.Model(&existing).Updates(map[string]any{"status": nextStatus, "error_message": ""}).Error; err != nil {
					return err
				}
				existing.Status = nextStatus
				existing.ErrorMessage = ""
			}
			operation = existing
			return nil
		}
		if filter.Direct {
			return checkOutstandingDirectReclaimsTx(tx, operationID, filter.OwnerUserIDs)
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

// ReclaimPending waits for completion for command-line and legacy callers.
// HTTP callers enqueue a task instead, so large audits do not hold a request.
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

// ProcessIncomeReclaimTask aggregates PostgreSQL settlements and transfers each
// owner's total once, atomically with the audit updates. Legacy non-PostgreSQL
// stores retain bounded batches. The task row is the cross-process mutex.
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
		reclaimAll := filter.MaxAmount == 0
		if filter.MaxAmount > 0 {
			available, err := reclaimableAmountTx(tx, filter)
			if err != nil {
				return err
			}
			remaining := filter.MaxAmount - task.Amount
			reclaimAll = available == remaining
			if available < remaining {
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
			now := time.Now().UTC()
			var cursorCreatedAt time.Time
			cursorID := ""
			for {
				allocationLimit := remaining - batchAmount
				if reclaimAll {
					allocationLimit = 0
				}
				amounts, err := reclaimPostgresTx(tx, filter, allocationLimit, now, cursorCreatedAt, cursorID)
				if err != nil {
					return err
				}
				updatedCount := 0
				for _, item := range amounts {
					ownerAmounts[item.OwnerUserID] += item.Amount
					batchAmount += item.Amount
					batchCount += item.Count
					updatedCount += item.Count
				}
				if len(amounts) > 0 {
					cursorCreatedAt, cursorID = amounts[0].CursorCreatedAt, amounts[0].CursorID
				}
				if updatedCount < reclaimBatchSize || (filter.MaxAmount > 0 && batchAmount >= remaining) {
					break
				}
			}
			if filter.MaxAmount > 0 && batchAmount != remaining {
				return errors.New("可回收收益发生变化，未扣除额度，请刷新后重试")
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
		if platformdb.UsingPostgreSQL || (filter.MaxAmount > 0 && newAmount == filter.MaxAmount) || batchCount < reclaimBatchSize {
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
		if recordErr := platformdb.DB.Model(&marketplaceschema.IncomeReclaim{}).Where("id = ? AND status <> ?", operationID, reclaimTaskCompleted).Updates(map[string]any{"status": reclaimTaskFailed, "error_message": err.Error()}).Error; recordErr != nil {
			return marketplaceschema.IncomeReclaim{}, errors.Join(err, fmt.Errorf("record reclaim failure: %w", recordErr))
		}
		return marketplaceschema.IncomeReclaim{}, err
	}
	return result, nil
}

// Audit rows are updated in bounded SQL batches to avoid million-row window
// spills. The caller accumulates owner totals within the same transaction and
// debits each wallet once after ALL audit updates, rather than once per batch.
func reclaimPostgresTx(tx *gorm.DB, filter ReleaseFilter, remaining int64, now time.Time, cursorCreatedAt time.Time, cursorID string) ([]reclaimBatchOwnerAmount, error) {
	locked := reclaimSettlementQuery(tx, filter).
		Select("id", "owner_user_id", "owner_net_amount", "reclaimed_amount", "created_at").
		Order("created_at ASC, id ASC").
		Limit(reclaimBatchSize).
		Clauses(clause.Locking{Strength: "UPDATE"})
	if cursorID != "" {
		locked = locked.Where("(created_at, id) > (?, ?)", cursorCreatedAt, cursorID)
	}
	allocation := "owner_net_amount - reclaimed_amount"
	if remaining > 0 {
		allocation = fmt.Sprintf(`LEAST(owner_net_amount - reclaimed_amount,
			GREATEST(%d::BIGINT - COALESCE(SUM(owner_net_amount - reclaimed_amount) OVER (
				ORDER BY created_at ASC, id ASC ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING), 0), 0))`, remaining)
	}
	table := marketplaceschema.Settlement{}.TableName()
	statement := fmt.Sprintf(`
WITH locked AS (?),
amounts AS (
	SELECT id, owner_user_id, owner_net_amount, reclaimed_amount,
		%s AS reclaim_amount
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
SELECT owner_user_id, COUNT(*) AS count, COALESCE(SUM(reclaim_amount), 0) AS amount,
	(SELECT created_at FROM locked ORDER BY created_at DESC, id DESC LIMIT 1) AS cursor_created_at,
	(SELECT id FROM locked ORDER BY created_at DESC, id DESC LIMIT 1) AS cursor_id
FROM updated
GROUP BY owner_user_id
ORDER BY owner_user_id`, allocation, table)
	var result []reclaimBatchOwnerAmount
	err := tx.Raw(statement, locked, now, statusReclaimed).Scan(&result).Error
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
		if item.ReferralRebatePending {
			return errors.New("marketplace referral allocation is pending")
		}
		if item.OwnerNetAmount == 0 {
			now := time.Now().UTC()
			return tx.Model(&item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
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
	if item.ReferralRebatePending {
		return errors.New("marketplace referral allocation is pending")
	}
	if item.OwnerNetAmount == 0 {
		now := time.Now().UTC()
		return tx.Model(item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
	}
	if err := forfeitHook(tx, item.PendingAccountID, 1, item.OwnerNetAmount, "marketplace-forfeit:"+item.ID); err != nil {
		return err
	}
	now := time.Now().UTC()
	return tx.Model(item).Updates(map[string]any{"status": statusForfeited, "forfeited_at": now}).Error
}

func releaseOne(settlementID string) error {
	return releaseOneContext(context.Background(), settlementID)
}

func releaseOwnerAccountBatchContext(ctx context.Context, ownerUserID int, batch releaseAccountBatch) (int, error) {
	processed := 0
	for offset := 0; offset < len(batch.settlementIDs); offset += releaseOwnerTransferBatchSize {
		end := min(offset+releaseOwnerTransferBatchSize, len(batch.settlementIDs))
		if err := releaseOwnerSettlementBatchContext(ctx, ownerUserID, batch.pendingAccountID, batch.settlementIDs[offset:end]); err != nil {
			return processed, err
		}
		processed += end - offset
	}
	return processed, nil
}

// releaseOwnerSettlementBatchContext debits one pending account and credits its
// owner once for the exact sum of the locked settlement rows.
func releaseOwnerSettlementBatchContext(ctx context.Context, ownerUserID int, pendingAccountID string, settlementIDs []string) error {
	if ownerUserID <= 0 || strings.TrimSpace(pendingAccountID) == "" || len(settlementIDs) == 0 {
		return errors.New("invalid marketplace release batch")
	}
	requestedIDs := slices.Clone(settlementIDs)
	slices.Sort(requestedIDs)
	return runMarketplaceReleaseTransaction(ctx, platformdb.DB, func(tx *gorm.DB) error {
		var items []marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", requestedIDs).Order("id asc").Find(&items).Error; err != nil {
			return err
		}
		if len(items) != len(requestedIDs) {
			return errors.New("marketplace release batch membership changed")
		}

		now := time.Now().UTC()
		activeIDs := make([]string, 0, len(items))
		var total int64
		for index := range items {
			item := &items[index]
			if item.OwnerUserID != ownerUserID || item.PendingAccountID != pendingAccountID {
				return errors.New("marketplace release batch ownership mismatch")
			}
			if item.Status != statusPending {
				continue
			}
			if item.ReferralRebatePending || item.AvailableAt.After(now) {
				return errors.New("marketplace release batch contains unavailable settlement")
			}
			if item.OwnerNetAmount < 0 {
				return errors.New("marketplace release batch contains negative owner amount")
			}
			if item.OwnerNetAmount > 0 && total > (1<<63-1)-item.OwnerNetAmount {
				return errors.New("marketplace release batch amount overflow")
			}
			activeIDs = append(activeIDs, item.ID)
			total += item.OwnerNetAmount
		}
		if len(activeIDs) == 0 {
			return nil
		}

		if total > 0 {
			batchKey := marketplaceReleaseBatchKey(ownerUserID, pendingAccountID, activeIDs)
			reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
				AccountID: pendingAccountID, RequestID: batchKey,
				ReservedAmount: total, IdempotencyKey: batchKey + ":reserve",
			})
			if err != nil {
				return err
			}
			if _, err = billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
				ReservationID: reservation.ReservationID, ActualAmount: total,
				IdempotencyKey: batchKey + ":settle",
			}); err != nil {
				return err
			}
			if err := releaseHook(tx, ownerUserID, total, batchKey+":credit", "marketplace_owner_release"); err != nil {
				return err
			}
		}

		updated := tx.Model(&marketplaceschema.Settlement{}).
			Where("id IN ? AND status = ?", activeIDs, statusPending).
			Updates(map[string]any{"status": statusReleased, "released_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != int64(len(activeIDs)) {
			return errors.New("marketplace release batch update count mismatch")
		}
		return nil
	})
}

func marketplaceReleaseBatchKey(ownerUserID int, pendingAccountID string, settlementIDs []string) string {
	ids := slices.Clone(settlementIDs)
	slices.Sort(ids)
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%d\x00%s\x00", ownerUserID, pendingAccountID)
	for _, settlementID := range ids {
		_, _ = hash.Write([]byte(settlementID))
		_, _ = hash.Write([]byte{0})
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	return "marketplace-release-batch:" + digest[:38]
}

func releaseOneContext(ctx context.Context, settlementID string) error {
	return runMarketplaceReleaseTransaction(ctx, platformdb.DB, func(tx *gorm.DB) error {
		var item marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", settlementID).Error; err != nil {
			return err
		}
		if item.Status != statusPending {
			return nil
		}
		if item.ReferralRebatePending {
			return errors.New("marketplace referral allocation is pending")
		}
		if item.OwnerNetAmount == 0 {
			now := time.Now().UTC()
			return tx.Model(&item).Updates(map[string]any{"status": statusReleased, "released_at": now}).Error
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

func runMarketplaceReleaseTransaction(ctx context.Context, db *gorm.DB, execute func(*gorm.DB) error) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := db.WithContext(ctx).Transaction(execute)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), err)
		}
		if attempt+1 >= releaseMaxAttempts || db.Dialector.Name() != "postgres" || !retryableMarketplaceReleaseError(err) {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * releaseRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), err)
		case <-timer.C:
		}
	}
}

func retryableMarketplaceReleaseError(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && (pgError.Code == "40P01" || pgError.Code == "40001")
}

func percentage(amount int64, percent int64) int64 {
	return roundedRatio(new(big.Int).Mul(big.NewInt(amount), big.NewInt(percent)), big.NewInt(100))
}

// creditPackCharges uses the purchase snapshot rather than today's plan price.
// Both amounts round half up once at the quota unit. V2 charges the purchase
// bonus/paid ratio; an unversioned historical event keeps its bonus/granted ratio.
func creditPackCharges(params RecordParams) (discount, fee int64, err error) {
	if params.FeePolicyVersion != "" && params.FeePolicyVersion != FeePolicyPaidBonusV2 {
		return 0, 0, errors.New("unsupported marketplace fee policy")
	}
	if params.CreditPackGrantedQuota == 0 && params.CreditPackPaidQuota == 0 && params.CreditPackChannelShareBPS == 0 {
		if params.BillingSource == "credit_pack" {
			return 0, 0, errors.New("credit pack settlement snapshot is missing")
		}
		return 0, 0, nil
	}
	if params.CreditPackGrantedQuota <= 0 || params.CreditPackPaidQuota <= 0 ||
		params.CreditPackPaidQuota > params.CreditPackGrantedQuota ||
		params.CreditPackChannelShareBPS < 0 || params.CreditPackChannelShareBPS > 10000 {
		return 0, 0, errors.New("invalid credit pack settlement snapshot")
	}
	numerator := new(big.Int).Mul(big.NewInt(params.SettlementGrossAmount), big.NewInt(params.CreditPackGrantedQuota-params.CreditPackPaidQuota))
	denominator := big.NewInt(params.CreditPackGrantedQuota)
	discount = roundedRatio(numerator, denominator)
	feeNumerator := new(big.Int).Mul(numerator, big.NewInt(params.CreditPackChannelShareBPS))
	if params.FeePolicyVersion == FeePolicyPaidBonusV2 {
		denominator = big.NewInt(params.CreditPackPaidQuota)
	}
	feeDenominator := new(big.Int).Mul(denominator, big.NewInt(10000))
	if feeNumerator.Cmp(new(big.Int).Mul(big.NewInt(params.SettlementGrossAmount), feeDenominator)) > 0 {
		return 0, 0, errors.New("credit pack fees exceed settlement gross amount")
	}
	return discount, roundedRatio(feeNumerator, feeDenominator), nil
}

// Callers provide nonnegative numerators and positive denominators whose
// quotient fits int64, even when their intermediate products do not.
func roundedRatio(numerator, denominator *big.Int) int64 {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient.Int64()
}
