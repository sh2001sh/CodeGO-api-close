package app

import (
	"errors"
	"sort"
	"strings"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FundingDailyEconomicsReport struct {
	Date              string                   `json:"date"`
	RecognizedRevenue float64                  `json:"recognized_revenue"`
	RecognizedCost    float64                  `json:"recognized_cost"`
	RecognizedProfit  float64                  `json:"recognized_profit"`
	UnattributedCost  float64                  `json:"unattributed_cost"`
	Sources           []FundingEconomicsSource `json:"sources"`
}

type FundingEconomicsSource struct {
	Source  string  `json:"source"`
	Quota   int64   `json:"quota"`
	Revenue float64 `json:"revenue"`
	Cost    float64 `json:"cost"`
	Profit  float64 `json:"profit"`
}

// RecordRequestEconomics snapshots procurement data separately from audit logs.
// The table is root-only and is safe to omit before its migration is applied.
func RecordRequestEconomics(relayInfo *gatewayruntime.RelayInfo, amount int) error {
	if relayInfo == nil || relayInfo.RequestId == "" || amount < 0 || relayInfo.ProcurementCostMultiplier <= 0 {
		return nil
	}
	if platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&billingschema.RequestEconomics{}) {
		return nil
	}
	channelID := 0
	if relayInfo.ChannelMeta != nil {
		channelID = relayInfo.ChannelId
	}
	revenueMultiplier := 0.0
	if relayInfo.CreditPackGrantedQuota > 0 {
		revenueMultiplier = float64(relayInfo.CreditPackPaidQuota) / float64(relayInfo.CreditPackGrantedQuota)
		if relayInfo.SubscriptionId > 0 {
			var sub commerceschema.UserSubscription
			if err := platformdb.DB.Select("source").First(&sub, relayInfo.SubscriptionId).Error; err != nil {
				return err
			}
			if sub.Source == "reset_redemption" {
				revenueMultiplier = 0
			}
		}
	} else if relayInfo.BillingSource == BillingSourceSubscription {
		var policy billingschema.FundingSourcePolicy
		if err := platformdb.DB.Where("source = ?", billingschema.FundingSourceSubscription).First(&policy).Error; err == nil {
			revenueMultiplier = policy.RevenueMultiplier
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	record := billingschema.RequestEconomics{
		RequestID:                 relayInfo.RequestId,
		ChannelID:                 channelID,
		RoutePoolID:               relayInfo.RoutePoolID,
		ActualAmount:              int64(amount),
		BillingSource:             relayInfo.BillingSource,
		SubscriptionID:            relayInfo.SubscriptionId,
		ProcurementCostMultiplier: relayInfo.ProcurementCostMultiplier,
		RevenueMultiplier:         revenueMultiplier,
		SettledAt:                 time.Now().UTC(),
	}
	return platformdb.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "request_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_id",
			"route_pool_id",
			"actual_amount",
			"billing_source",
			"subscription_id",
			"procurement_cost_multiplier",
			"revenue_multiplier",
			"settled_at",
		}),
	}).Create(&record).Error
}

// FundingAttributionPolicies returns root-managed source multipliers. A zero
// multiplier means the source is deliberately excluded from realized revenue.
func FundingAttributionPolicies() ([]billingschema.FundingSourcePolicy, error) {
	var policies []billingschema.FundingSourcePolicy
	err := platformdb.DB.Order("source asc").Find(&policies).Error
	return policies, err
}

func SaveFundingAttributionPolicies(policies []billingschema.FundingSourcePolicy) error {
	for _, policy := range policies {
		if !isSupportedFundingSource(policy.Source) || policy.RevenueMultiplier < 0 {
			return errors.New("invalid funding source policy")
		}
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		for _, policy := range policies {
			if err := tx.Save(&policy).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DailyFundingEconomics joins settled route procurement with immutable funding
// allocations. Values without a recognized origin remain visible as cost only.
func DailyFundingEconomics(day time.Time, quotaPerUnit float64) (*FundingDailyEconomicsReport, error) {
	if quotaPerUnit <= 0 {
		return nil, errors.New("quota per unit must be positive")
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return nil, err
	}
	start := time.Date(day.In(location).Year(), day.In(location).Month(), day.In(location).Day(), 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	result := &FundingDailyEconomicsReport{
		Date:    start.Format("2006-01-02"),
		Sources: make([]FundingEconomicsSource, 0),
	}
	if platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&billingschema.RequestEconomics{}) {
		return result, nil
	}
	var records []billingschema.RequestEconomics
	if err := platformdb.DB.Where("settled_at >= ? AND settled_at < ?", start.UTC(), end.UTC()).Find(&records).Error; err != nil {
		return nil, err
	}
	requestIDs := make([]string, 0, len(records))
	for _, record := range records {
		if record.BillingSource != BillingSourceSubscription {
			requestIDs = append(requestIDs, record.RequestID)
		}
	}
	allocationsByRequest := make(map[string][]billingschema.FundingAllocation)
	if len(requestIDs) > 0 && platformdb.DB.Migrator().HasTable(&billingschema.FundingAllocation{}) {
		var allocations []billingschema.FundingAllocation
		if err := platformdb.DB.Where("request_id IN ?", requestIDs).Find(&allocations).Error; err != nil {
			return nil, err
		}
		for _, allocation := range allocations {
			allocationsByRequest[allocation.RequestID] = append(allocationsByRequest[allocation.RequestID], allocation)
		}
	}
	sources := make(map[string]*FundingEconomicsSource)
	for _, record := range records {
		baseCost := float64(record.ActualAmount) / quotaPerUnit * record.ProcurementCostMultiplier
		if record.BillingSource == BillingSourceSubscription {
			appendFundingEconomics(sources, billingschema.FundingSourceSubscription, record.ActualAmount,
				float64(record.ActualAmount)/quotaPerUnit*record.RevenueMultiplier, baseCost)
			continue
		}
		allocations := allocationsByRequest[record.RequestID]
		if len(allocations) == 0 {
			appendFundingEconomics(sources, billingschema.FundingSourceLegacyUnattributed, record.ActualAmount, 0, baseCost)
			continue
		}
		for _, allocation := range allocations {
			cost := baseCost * float64(allocation.Amount) / float64(record.ActualAmount)
			appendFundingEconomics(sources, allocation.Source, allocation.Amount,
				float64(allocation.Amount)/quotaPerUnit*allocation.RevenueMultiplier, cost)
		}
	}
	for _, source := range sources {
		source.Profit = source.Revenue - source.Cost
		result.Sources = append(result.Sources, *source)
		if source.Source == billingschema.FundingSourceLegacyUnattributed || source.Source == billingschema.FundingSourceOther {
			result.UnattributedCost += source.Cost
			continue
		}
		result.RecognizedRevenue += source.Revenue
		result.RecognizedCost += source.Cost
	}
	result.RecognizedProfit = result.RecognizedRevenue - result.RecognizedCost
	sort.Slice(result.Sources, func(i, j int) bool { return result.Sources[i].Source < result.Sources[j].Source })
	return result, nil
}

func appendFundingEconomics(sources map[string]*FundingEconomicsSource, source string, quota int64, revenue, cost float64) {
	entry := sources[source]
	if entry == nil {
		entry = &FundingEconomicsSource{Source: source}
		sources[source] = entry
	}
	entry.Quota += quota
	entry.Revenue += revenue
	entry.Cost += cost
}

func recordFundingLotTx(tx *gorm.DB, accountID string, amount int64, idempotencyKey, reasonCode, referenceType, referenceID string) error {
	if tx == nil || amount <= 0 || strings.TrimSpace(accountID) == "" {
		return nil
	}
	if !tx.Migrator().HasTable(&billingschema.FundingLot{}) {
		return nil
	}
	var existing billingschema.FundingLot
	err := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	source := fundingSourceFromReason(reasonCode)
	multiplier := 0.0
	if reasonCode != "referral_consumption_rebate" && reasonCode != "subscription_conversion_base" && reasonCode != "subscription_conversion_gift" && tx.Migrator().HasTable(&billingschema.FundingSourcePolicy{}) {
		var policy billingschema.FundingSourcePolicy
		if err := tx.Where("source = ?", source).First(&policy).Error; err == nil {
			multiplier = policy.RevenueMultiplier
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return tx.Create(&billingschema.FundingLot{
		AccountID:         accountID,
		Source:            source,
		ReferenceType:     referenceType,
		ReferenceID:       referenceID,
		OriginalAmount:    amount,
		RemainingAmount:   amount,
		RevenueMultiplier: multiplier,
		IdempotencyKey:    idempotencyKey,
	}).Error
}

// AllocateSettledFundingFIFO records the actual wallet source consumed by a
// request. It is idempotent and serializes lot updates with database row locks.
func AllocateSettledFundingFIFO(requestID, accountID string, amount int64) error {
	if requestID == "" || accountID == "" || amount <= 0 {
		return nil
	}
	if platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&billingschema.FundingAllocation{}) {
		return nil
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return allocateSettledFundingFIFOTx(tx, requestID, accountID, amount)
	})
}

func allocateSettledFundingFIFOTx(tx *gorm.DB, requestID, accountID string, amount int64) error {
	// Both the relay and outbox retry use the account lock before reading
	// allocations, so one request cannot decrement its lots twice.
	var account billingschema.BillingAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("account_id").Where("account_id = ?", accountID).Find(&account).Error; err != nil {
		return err
	}
	var allocations int64
	if err := tx.Model(&billingschema.FundingAllocation{}).Where("request_id = ?", requestID).Count(&allocations).Error; err != nil {
		return err
	}
	if allocations > 0 {
		return nil
	}
	remaining := amount
	var cursor *fundingFIFOBatch
	for remaining > 0 {
		if tx.Dialector.Name() == "postgres" {
			batch, err := allocateSettledFundingPostgresBatchTx(tx, requestID, accountID, remaining, cursor)
			if err != nil {
				return err
			}
			if batch.Used > 0 {
				remaining -= batch.Used
				cursor = &batch
				continue
			}
		}
		var lot billingschema.FundingLot
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("account_id = ? AND remaining_amount > 0", accountID).
			Order("created_at asc, lot_id asc").Take(&lot).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lot = billingschema.FundingLot{
				AccountID: accountID, Source: billingschema.FundingSourceLegacyUnattributed,
				ReferenceType: "legacy_wallet", ReferenceID: requestID,
				OriginalAmount: remaining, RemainingAmount: remaining,
				RevenueMultiplier: 0, IdempotencyKey: "legacy-wallet:" + accountID + ":" + requestID,
			}
			if err := tx.Create(&lot).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		used := lot.RemainingAmount
		if used > remaining {
			used = remaining
		}
		paid := int64(0)
		if lot.PaidQuota > 0 && lot.PaidQuota <= lot.OriginalAmount && lot.RemainingAmount <= lot.OriginalAmount {
			consumed := lot.OriginalAmount - lot.RemainingAmount
			paid = floorPaidQuota(consumed+used, lot.PaidQuota, lot.OriginalAmount) - floorPaidQuota(consumed, lot.PaidQuota, lot.OriginalAmount)
		}
		if err := tx.Model(&lot).Update("remaining_amount", gorm.Expr("remaining_amount - ?", used)).Error; err != nil {
			return err
		}
		if err := tx.Create(&billingschema.FundingAllocation{
			RequestID: requestID, LotID: lot.LotID, AccountID: accountID,
			Source: lot.Source, Amount: used, PaidQuota: paid, RevenueMultiplier: lot.RevenueMultiplier,
		}).Error; err != nil {
			return err
		}
		remaining -= used
	}
	return nil
}

// Keep the existing account lock and all per-lot attribution in the caller's
// transaction. Only a bounded FIFO prefix is locked/materialized in PostgreSQL;
// no lot rows cross the application/database connection. This avoids three
// network round trips per micro-lot while preserving exact paid-quota rounding.
// The cursor only lives inside this account-locked transaction: exhausted lots
// remain in the partial index until cleanup, so restarting each batch at its
// beginning repeatedly visits the transaction's own dead index entries.
type fundingFIFOBatch struct {
	Used          int64
	CreatedAt     time.Time
	LotID         string
	ExpectedCount int64
	ExpectedUsed  int64
	UpdatedCount  int64
	UpdatedUsed   int64
	InsertedCount int64
}

func (batch fundingFIFOBatch) complete() bool {
	return batch.ExpectedCount == batch.UpdatedCount && batch.ExpectedCount == batch.InsertedCount &&
		batch.ExpectedUsed == batch.UpdatedUsed && batch.ExpectedUsed == batch.Used
}

func allocateSettledFundingPostgresBatchTx(tx *gorm.DB, requestID, accountID string, amount int64, after *fundingFIFOBatch) (fundingFIFOBatch, error) {
	const savepoint = "funding_fifo_ctid_batch"
	var empty fundingFIFOBatch
	if err := tx.SavePoint(savepoint).Error; err != nil {
		return empty, err
	}
	batch, err := allocateSettledFundingPostgresStatementTx(tx, requestID, accountID, amount, after, true)
	if err != nil {
		return empty, err
	}
	if !batch.complete() {
		// A row that changed while SELECT waited for FOR UPDATE can retain its
		// old projected ctid. Discard every write in this batch before retrying
		// by stable primary key; never advance the cursor from a partial update.
		if err := tx.RollbackTo(savepoint).Error; err != nil {
			return empty, err
		}
		if err := tx.Exec("RELEASE SAVEPOINT " + savepoint).Error; err != nil {
			return empty, err
		}
		batch, err = allocateSettledFundingPostgresStatementTx(tx, requestID, accountID, amount, after, false)
		if err != nil {
			return empty, err
		}
		if !batch.complete() {
			return empty, errors.New("funding FIFO batch attribution is incomplete")
		}
		return batch, nil
	}
	if err := tx.Exec("RELEASE SAVEPOINT " + savepoint).Error; err != nil {
		return empty, err
	}
	return batch, nil
}

func allocateSettledFundingPostgresStatementTx(tx *gorm.DB, requestID, accountID string, amount int64, after *fundingFIFOBatch, physical bool) (fundingFIFOBatch, error) {
	query := `WITH candidates AS MATERIALIZED (
		SELECT `
	if physical {
		query += `ctid AS row_tid, `
	}
	query += `lot_id, account_id, source, original_amount, paid_quota,
			remaining_amount, revenue_multiplier, created_at
		FROM billing.funding_lots
		WHERE account_id = ? AND remaining_amount > 0`
	args := []any{accountID}
	if after != nil {
		query += ` AND (created_at, lot_id) > (?, ?)`
		args = append(args, after.CreatedAt, after.LotID)
	}
	query += `
		ORDER BY created_at ASC, lot_id ASC LIMIT 4096 FOR UPDATE
	), ranked AS (
		SELECT *, COALESCE(SUM(remaining_amount::numeric) OVER (
			ORDER BY created_at ASC, lot_id ASC
			ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING), 0) AS prior_amount
		FROM candidates
	), consumed AS (
		SELECT *, LEAST(remaining_amount::numeric, ?::numeric - prior_amount)::bigint AS used
		FROM ranked WHERE prior_amount < ?::numeric
	), updated AS (
		UPDATE billing.funding_lots AS lot
		SET remaining_amount = lot.remaining_amount - consumed.used
		FROM consumed WHERE `
	if physical {
		query += `lot.ctid = consumed.row_tid AND `
	}
	query += `lot.lot_id = consumed.lot_id
		RETURNING consumed.lot_id, consumed.account_id, consumed.source,
			consumed.original_amount, consumed.paid_quota, consumed.remaining_amount,
			consumed.revenue_multiplier, consumed.used
	), inserted AS (
		INSERT INTO billing.funding_allocations (
			allocation_id, request_id, lot_id, account_id, source, amount,
			paid_quota, revenue_multiplier, created_at)
		SELECT replace(gen_random_uuid()::text, '-', ''), ?, lot_id, account_id,
			source, used, CASE WHEN paid_quota > 0 AND paid_quota <= original_amount
				AND remaining_amount <= original_amount
			THEN (div((original_amount - remaining_amount + used)::numeric * paid_quota, original_amount)
				- div((original_amount - remaining_amount)::numeric * paid_quota, original_amount))::bigint
			ELSE 0 END, revenue_multiplier, clock_timestamp()
		FROM updated RETURNING amount
	)
	SELECT totals.used, totals.inserted_count, last_lot.created_at, last_lot.lot_id,
		expected.expected_count, expected.expected_used, actual.updated_count, actual.updated_used
	FROM (SELECT COALESCE(SUM(amount), 0)::bigint AS used, COUNT(*) AS inserted_count FROM inserted) totals
	CROSS JOIN (SELECT COUNT(*) AS expected_count, COALESCE(SUM(used), 0)::bigint AS expected_used FROM consumed) expected
	CROSS JOIN (SELECT COUNT(*) AS updated_count, COALESCE(SUM(used), 0)::bigint AS updated_used FROM updated) actual
	LEFT JOIN (SELECT created_at, lot_id FROM consumed ORDER BY created_at DESC, lot_id DESC LIMIT 1) last_lot ON true`
	var result fundingFIFOBatch
	args = append(args, amount, amount, requestID)
	err := tx.Raw(query, args...).Scan(&result).Error
	return result, err
}

func fundingSourceFromReason(reasonCode string) string {
	reasonCode = strings.ToLower(strings.TrimSpace(reasonCode))
	switch {
	case strings.Contains(reasonCode, "topup") || strings.Contains(reasonCode, "top_up"):
		return billingschema.FundingSourceTopup
	case strings.Contains(reasonCode, "blind_box") || strings.Contains(reasonCode, "blind-box"):
		return billingschema.FundingSourceBlindBox
	default:
		return billingschema.FundingSourceOther
	}
}

func isSupportedFundingSource(source string) bool {
	switch strings.TrimSpace(source) {
	case billingschema.FundingSourceTopup, billingschema.FundingSourceBlindBox, billingschema.FundingSourceSubscription:
		return true
	default:
		return false
	}
}
