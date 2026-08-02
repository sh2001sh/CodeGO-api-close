package app

import (
	"errors"
	"sort"
	"strings"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
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
	if relayInfo.BillingSource == BillingSourceSubscription {
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
	if tx.Migrator().HasTable(&billingschema.FundingSourcePolicy{}) {
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
		var allocations int64
		if err := tx.Model(&billingschema.FundingAllocation{}).Where("request_id = ?", requestID).Count(&allocations).Error; err != nil {
			return err
		}
		if allocations > 0 {
			return nil
		}
		if err := ensureLegacyFundingLotTx(tx, accountID, amount); err != nil {
			return err
		}
		var lots []billingschema.FundingLot
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("account_id = ? AND remaining_amount > 0", accountID).
			Order("created_at asc, lot_id asc").Find(&lots).Error; err != nil {
			return err
		}
		remaining := amount
		for index := range lots {
			if remaining == 0 {
				break
			}
			used := lots[index].RemainingAmount
			if used > remaining {
				used = remaining
			}
			if err := tx.Model(&lots[index]).Update("remaining_amount", gorm.Expr("remaining_amount - ?", used)).Error; err != nil {
				return err
			}
			if err := tx.Create(&billingschema.FundingAllocation{
				RequestID: requestID, LotID: lots[index].LotID, AccountID: accountID,
				Source: lots[index].Source, Amount: used, RevenueMultiplier: lots[index].RevenueMultiplier,
			}).Error; err != nil {
				return err
			}
			remaining -= used
		}
		if remaining != 0 {
			return errors.New("funding lot balance is lower than settled wallet amount")
		}
		return nil
	})
}

func ensureLegacyFundingLotTx(tx *gorm.DB, accountID string, minimumAmount int64) error {
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Where("account_id = ?", accountID).First(&snapshot).Error; err != nil {
		return err
	}
	var totalAvailable int64
	if err := tx.Model(&billingschema.FundingLot{}).Where("account_id = ?", accountID).
		Select("COALESCE(SUM(remaining_amount), 0)").Scan(&totalAvailable).Error; err != nil {
		return err
	}
	target := snapshot.AvailableBalance + minimumAmount
	if totalAvailable >= target {
		return nil
	}
	missing := target - totalAvailable
	return tx.Create(&billingschema.FundingLot{
		AccountID: accountID, Source: billingschema.FundingSourceLegacyUnattributed,
		ReferenceType: "legacy_wallet", ReferenceID: accountID, OriginalAmount: missing, RemainingAmount: missing,
		RevenueMultiplier: 0, IdempotencyKey: "legacy-wallet:" + accountID + ":" + platformruntime.GetUUID(),
	}).Error
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
