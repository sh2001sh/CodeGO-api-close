package app

import (
	"encoding/json"
	"errors"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const marketplaceUsageSettledEvent = "billing.marketplace_usage_settled"

// The immutable usage intent commits with the native debit. The existing inline
// projection remains fast, while the outbox guarantees repair after a timeout,
// a process exit, or a failed inline marketplace transaction.
func marketplaceSettlementUsage(info *relaycommon.RelayInfo, amount int) (*auditschema.Log, error) {
	if info == nil || info.MarketplaceGroupID == "" || amount <= 0 {
		return nil, nil
	}
	if info.RequestId == "" || info.MarketplaceOwnerID <= 0 || info.UserId <= 0 || info.ChannelMeta == nil || info.ChannelId <= 0 {
		return nil, errors.New("marketplace settlement routing evidence is missing")
	}
	other := make(map[string]interface{})
	appendBillingInfo(info, other)
	encoded, err := json.Marshal(other)
	if err != nil {
		return nil, err
	}
	return &auditschema.Log{RequestId: info.RequestId, UserId: info.UserId, ChannelId: info.ChannelId,
		Type: auditschema.LogTypeConsume, Quota: amount, CreatedAt: time.Now().UTC().Unix(), Other: string(encoded)}, nil
}

func recordMarketplaceUsageTx(tx *gorm.DB, accountID string, usage *auditschema.Log) error {
	if usage == nil {
		return nil
	}
	return billingdomain.RecordOutboxEvent(tx, billingdomain.OutboxEventInput{
		AccountID: accountID, AggregateType: "marketplace_usage", AggregateID: usage.RequestId,
		EventType: marketplaceUsageSettledEvent, IdempotencyKey: "outbox:marketplace-usage:" + usage.RequestId,
		Payload: usage,
	})
}

func consumeMarketplaceUsageTx(tx *gorm.DB, event billingschema.BillingOutboxEvent) error {
	if event.EventType != marketplaceUsageSettledEvent {
		return nil
	}
	var usage auditschema.Log
	if err := json.Unmarshal(event.Payload, &usage); err != nil {
		return err
	}
	if usage.RequestId == "" || event.AggregateID != usage.RequestId {
		return errors.New("marketplace settlement outbox identity mismatch")
	}
	var count int64
	if err := tx.Model(&billingschema.BillingReservation{}).
		Where("request_id = ? AND account_id = ?", usage.RequestId, event.AccountID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errors.New("marketplace settlement outbox funding account mismatch")
	}
	_, err := marketplacesettlement.RecoverUsageTx(tx, usage)
	if errors.Is(err, marketplacesettlement.ErrUsageNoIncome) {
		return nil
	}
	return err
}

func (s *SubscriptionFunding) settleMarketplaceUsage(info *relaycommon.RelayInfo, amount int) error {
	if subscriptionFundingHooks.SettleReservationTx == nil {
		return errors.New("transactional subscription settlement hook is not registered")
	}
	usage, err := marketplaceSettlementUsage(info, amount)
	if err != nil {
		return err
	}
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := subscriptionFundingHooks.SettleReservationTx(tx, s.requestID, s.subscriptionID, s.modelName, int64(amount)); err != nil {
			return err
		}
		return recordMarketplaceUsageTx(tx, s.accountID, usage)
	})
	if err != nil {
		return err
	}
	return s.loadSettlement()
}
