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
	MarketplaceGroupID     string  `json:"marketplace_group_id"`
	MarketplaceMultiplier  float64 `json:"marketplace_multiplier"`
	BillingSource          string  `json:"billing_source"`
	SubscriptionMultiplier float64 `json:"subscription_multiplier"`
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
			Where("id > ? AND type = ? AND request_id <> '' AND other LIKE ?", lastID, auditschema.LogTypeConsume, "%\"marketplace_group_id\"%").
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
		if !apply {
			continue
		}
		gross := settlementBackfillGross(int64(log.Quota), payload.BillingSource)
		multiplier := payload.MarketplaceMultiplier
		if multiplier <= 0 {
			multiplier = group.Multiplier
		}
		err := marketplacesettlement.Record(marketplacesettlement.RecordParams{
			RequestID: log.RequestId, GroupID: group.ID, OwnerUserID: group.OwnerUserID,
			ConsumerUserID: log.UserId, BillingSource: payload.BillingSource,
			ConsumerDebitAmount: int64(log.Quota), SettlementGrossAmount: int64(gross),
			WalletMultiplier: multiplier, SubscriptionMultiplier: payload.SubscriptionMultiplier,
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
