package app

import (
	"encoding/json"
	"errors"
	"strings"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

var ErrPrimaryDatabaseNotInitialized = errors.New("primary database is not initialized")

const settlementBackfillBatchSize = 5000

// SettlementBackfillReport describes successful marketplace usage logs that
// were missing a marketplace settlement row and were inspected or repaired.
type SettlementBackfillReport struct {
	Candidates int
	Created    int
	Existing   int
	Skipped    int
	Invalid    int
}

type settlementBackfillPayload struct {
	MarketplaceGroupID         string  `json:"marketplace_group_id"`
	MarketplaceMultiplier      float64 `json:"marketplace_multiplier"`
	BillingSource              string  `json:"billing_source"`
	SubscriptionMultiplier     float64 `json:"subscription_multiplier"`
	SubscriptionBillingVersion string  `json:"subscription_billing_version"`
	CreditPackGrantedQuota     int64   `json:"credit_pack_granted_quota"`
	CreditPackPaidQuota        int64   `json:"credit_pack_paid_quota"`
	CreditPackChannelShareBPS  *int64  `json:"credit_pack_channel_share_bps"`
	FeePolicyVersion           string  `json:"marketplace_fee_policy_version"`
	SubscriptionID             int     `json:"subscription_id"`
	SubscriptionPlanID         int     `json:"subscription_plan_id"`
	SubscriptionPlanTitle      string  `json:"subscription_plan_title"`
}

// BackfillMissingSettlements inspects successful request logs carrying the
// immutable marketplace routing metadata and creates any missing settlement
// rows. It is idempotent and intentionally refuses to infer ownership from the
// display group name or from a channel that does not match the log channel ID.
func BackfillMissingSettlements(limit int, apply bool) (SettlementBackfillReport, error) {
	var report SettlementBackfillReport
	logDB := platformdb.LogDB
	if logDB == nil {
		logDB = platformdb.DB
	}
	if logDB == nil || platformdb.DB == nil {
		return report, ErrPrimaryDatabaseNotInitialized
	}

	lastID := 0
	inspected := 0
	for {
		batchLimit := settlementBackfillBatchSize
		if limit > 0 && limit-inspected < batchLimit {
			batchLimit = limit - inspected
		}
		if batchLimit <= 0 {
			break
		}
		var logs []auditschema.Log
		err := logDB.Model(&auditschema.Log{}).
			Where("id > ? AND type = ? AND request_id <> ''", lastID, auditschema.LogTypeConsume).
			Order("id asc").Limit(batchLimit).Find(&logs).Error
		if err != nil {
			return report, err
		}
		if len(logs) == 0 {
			break
		}
		lastID = logs[len(logs)-1].Id
		inspected += len(logs)
		if err := backfillSettlementBatch(logs, apply, &report); err != nil {
			return report, err
		}
		if len(logs) < batchLimit {
			break
		}
	}
	return report, nil
}

func backfillSettlementBatch(logs []auditschema.Log, apply bool, report *SettlementBackfillReport) error {
	candidates := make([]auditschema.Log, 0, len(logs))
	for _, log := range logs {
		if strings.Contains(log.Other, `"marketplace_group_id"`) {
			candidates = append(candidates, log)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	logs = candidates
	requestIDs := make([]string, 0, len(logs))
	groupIDs := make([]string, 0, len(logs))
	seenGroups := make(map[string]struct{}, len(logs))
	for _, log := range logs {
		requestIDs = append(requestIDs, log.RequestId)
		var payload settlementBackfillPayload
		if json.Unmarshal([]byte(log.Other), &payload) == nil && strings.TrimSpace(payload.MarketplaceGroupID) != "" {
			if _, exists := seenGroups[payload.MarketplaceGroupID]; !exists {
				seenGroups[payload.MarketplaceGroupID] = struct{}{}
				groupIDs = append(groupIDs, payload.MarketplaceGroupID)
			}
		}
	}
	var existing []marketplaceschema.Settlement
	if err := platformdb.DB.Where("request_id IN ?", requestIDs).Find(&existing).Error; err != nil {
		return err
	}
	existingByRequest := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		existingByRequest[item.RequestID] = struct{}{}
	}
	var groups []marketplaceschema.Group
	if err := platformdb.DB.Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
		return err
	}
	groupsByID := make(map[string]marketplaceschema.Group, len(groups))
	channelIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupsByID[group.ID] = group
		channelIDs = append(channelIDs, group.ChannelID)
	}
	var channels []marketplaceschema.Channel
	if len(channelIDs) > 0 {
		if err := platformdb.DB.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
			return err
		}
	}
	channelsByID := make(map[string]marketplaceschema.Channel, len(channels))
	for _, channel := range channels {
		channelsByID[channel.ID] = channel
	}

	for _, log := range logs {
		if _, exists := existingByRequest[log.RequestId]; exists {
			report.Existing++
			continue
		}
		report.Candidates++
		var payload settlementBackfillPayload
		if err := json.Unmarshal([]byte(log.Other), &payload); err != nil || strings.TrimSpace(payload.MarketplaceGroupID) == "" {
			report.Invalid++
			continue
		}
		group, ok := groupsByID[payload.MarketplaceGroupID]
		channel, channelOK := channelsByID[group.ChannelID]
		if !ok || !channelOK || channel.InternalChannelID == nil || *channel.InternalChannelID != log.ChannelId || group.OwnerUserID <= 0 || log.Quota <= 0 {
			report.Skipped++
			continue
		}
		creditPack := payload.SubscriptionBillingVersion == "credit_pack" || payload.CreditPackGrantedQuota > 0 || payload.BillingSource == "credit_pack"
		if creditPack && (payload.CreditPackGrantedQuota <= 0 || payload.CreditPackPaidQuota <= 0 ||
			payload.CreditPackPaidQuota > payload.CreditPackGrantedQuota || payload.CreditPackChannelShareBPS == nil ||
			*payload.CreditPackChannelShareBPS < 0 || *payload.CreditPackChannelShareBPS > 10000 ||
			(payload.FeePolicyVersion != "" && payload.FeePolicyVersion != marketplacesettlement.FeePolicyPaidBonusV2)) {
			report.Invalid++
			continue
		}
		gross := settlementBackfillGross(int64(log.Quota), payload.BillingSource)
		if creditPack {
			gross = int64(log.Quota)
		}
		if gross <= 0 {
			report.Skipped++
			continue
		}
		if !apply {
			continue
		}
		multiplier := payload.MarketplaceMultiplier
		if multiplier <= 0 && !creditPack {
			multiplier = group.Multiplier
		}
		share := int64(0)
		if payload.CreditPackChannelShareBPS != nil {
			share = *payload.CreditPackChannelShareBPS
		}
		err := marketplacesettlement.Record(marketplacesettlement.RecordParams{
			RequestID: log.RequestId, GroupID: group.ID, OwnerUserID: group.OwnerUserID,
			ConsumerUserID: log.UserId, BillingSource: payload.BillingSource,
			ConsumerDebitAmount: int64(log.Quota), SettlementGrossAmount: int64(gross),
			WalletMultiplier: multiplier, SubscriptionMultiplier: payload.SubscriptionMultiplier,
			CreditPackGrantedQuota: payload.CreditPackGrantedQuota, CreditPackPaidQuota: payload.CreditPackPaidQuota,
			CreditPackChannelShareBPS: share, FeePolicyVersion: payload.FeePolicyVersion,
			SubscriptionID: payload.SubscriptionID, SubscriptionPlanID: payload.SubscriptionPlanID,
			SubscriptionPlanTitle: payload.SubscriptionPlanTitle,
		})
		if err != nil {
			return err
		}
		report.Created++
		existingByRequest[log.RequestId] = struct{}{}
	}
	return nil
}

func settlementBackfillGross(quota int64, billingSource string) int64 {
	if quota <= 0 {
		return 0
	}
	if billingSource != "subscription" {
		return quota
	}
	return (quota + 5) / 10
}
