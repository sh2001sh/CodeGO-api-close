package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

type creditPackReferralContext struct {
	OriginalRequestID string                             `json:"original_request_id"`
	FundingRequestID  string                             `json:"funding_request_id"`
	AccountID         string                             `json:"account_id"`
	ConsumerID        int                                `json:"consumer_id"`
	ChannelID         int                                `json:"channel_id"`
	ChannelScope      string                             `json:"channel_scope"`
	FundingSource     string                             `json:"funding_source"`
	SettlementIDs     []string                           `json:"settlement_ids"`
	Marketplace       marketplacesettlement.RecordParams `json:"marketplace"`
}

// settleCreditPackWithReferral commits final route evidence together with every
// native settlement. No settled event can become visible without its funding
// evidence, including when persistence fails or the gateway exits before commit.
func (s *SubscriptionFunding) settleCreditPackWithReferral(info *relaycommon.RelayInfo, amount int) error {
	if subscriptionFundingHooks.SettleReservationTx == nil {
		return errors.New("transactional subscription settlement hook is not registered")
	}
	if info == nil || info.CreditPackGrantedQuota <= 0 || s.reservationID == "" {
		return errors.New("credit pack settlement evidence is missing")
	}
	var settlementID string
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := subscriptionFundingHooks.SettleReservationTx(tx, s.requestID, s.subscriptionID, s.modelName, int64(amount)); err != nil {
			return err
		}
		if err := attachCreditPackReferralContextTx(tx, info, s, amount); err != nil {
			return err
		}
		var settlement billingschema.BillingSettlement
		if err := tx.First(&settlement, "reservation_id = ?", s.reservationID).Error; err != nil {
			return err
		}
		settlementID = settlement.SettlementID
		return nil
	})
	if err == nil {
		s.settlementID = settlementID
	}
	return err
}

// attachCreditPackReferralContext supports idempotent reconciliation of existing
// evidence. Live relay settlement uses the transaction-scoped variant above.
func attachCreditPackReferralContext(info *relaycommon.RelayInfo, amount int) error {
	if info == nil || info.CreditPackGrantedQuota <= 0 || amount <= 0 {
		return nil
	}
	session, ok := info.Billing.(*BillingSession)
	if !ok {
		return errors.New("credit pack referral session is missing")
	}
	funding, _ := session.funding.(*SubscriptionFunding)
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return attachCreditPackReferralContextTx(tx, info, funding, amount)
	})
}

func attachCreditPackReferralContextTx(tx *gorm.DB, info *relaycommon.RelayInfo, funding *SubscriptionFunding, amount int) error {
	if info == nil || info.CreditPackGrantedQuota <= 0 || amount <= 0 {
		return nil
	}
	if funding == nil || funding.requestID == "" || funding.accountID == "" || info.RequestId == "" || info.ChannelMeta == nil || info.ChannelId <= 0 {
		return errors.New("credit pack referral route evidence is missing")
	}
	ctx := creditPackReferralContext{OriginalRequestID: info.RequestId, FundingRequestID: funding.requestID, AccountID: funding.accountID, ConsumerID: info.UserId, ChannelID: info.ChannelId, ChannelScope: info.ChannelScope, Marketplace: marketplaceSettlementParams(info, amount)}
	if info.MarketplaceGroupID != "" && info.MarketplaceOwnerID > 0 {
		ctx.FundingSource = "marketplace_channel"
	} else if info.MarketplaceGroupID == "" && info.MarketplaceOwnerID == 0 && info.ChannelScope == gatewayschema.ChannelScopeOfficial {
		ctx.FundingSource = "official_channel"
	} else {
		return errors.New("credit pack referral funding route is ambiguous")
	}
	var settlements []billingschema.BillingSettlement
	table := (billingschema.BillingSettlement{}).TableName()
	reservations := (billingschema.BillingReservation{}).TableName()
	if err := tx.Table(table+" AS s").Select("s.*").Joins("JOIN "+reservations+" AS r ON r.reservation_id=s.reservation_id").
		Where("r.request_id = ? AND r.account_id = ? AND s.status = ? AND s.actual_amount > 0", funding.requestID, funding.accountID, billingschema.BillingSettlementStatusCompleted).Order("s.settlement_id asc").Find(&settlements).Error; err != nil {
		return err
	}
	var total int64
	for _, settlement := range settlements {
		if settlement.ActualAmount > int64(amount)-total {
			return errors.New("credit pack referral settlement total mismatch")
		}
		total += settlement.ActualAmount
		ctx.SettlementIDs = append(ctx.SettlementIDs, settlement.SettlementID)
	}
	if total != int64(amount) || len(ctx.SettlementIDs) == 0 {
		return errors.New("credit pack referral settlements are incomplete")
	}
	for _, settlement := range settlements {
		var event billingschema.BillingOutboxEvent
		// The idempotency key already has a unique index. Scanning by the
		// unindexed aggregate ID holds subscription locks while reading all events.
		if err := tx.First(&event, "idempotency_key = ?", "outbox:"+settlement.IdempotencyKey).Error; err != nil {
			return err
		}
		if event.AggregateID != settlement.SettlementID || event.EventType != "billing.reservation_settled" || event.AccountID != funding.accountID {
			return errors.New("credit pack settlement outbox identity mismatch")
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		encoded, err := json.Marshal(ctx)
		if err != nil {
			return err
		}
		if prior := payload["referral_context"]; len(prior) > 0 {
			var existing creditPackReferralContext
			if err := json.Unmarshal(prior, &existing); err != nil {
				return err
			}
			previous, _ := json.Marshal(existing)
			if string(previous) != string(encoded) {
				return errors.New("credit pack referral route context is immutable")
			}
			continue
		}
		if event.Status != billingschema.BillingOutboxStatusPending {
			return errors.New("credit pack referral context arrived after event publication")
		}
		payload["referral_context"] = encoded
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := tx.Model(&event).Update("payload", data).Error; err != nil {
			return err
		}
	}
	return nil
}

func consumeReferralSettlementTx(tx *gorm.DB, event billingschema.BillingOutboxEvent, creditedInviters map[int]struct{}) error {
	if event.EventType != "billing.reservation_settled" {
		return nil
	}
	var native billingschema.BillingSettlement
	if err := tx.First(&native, "settlement_id = ?", event.AggregateID).Error; err != nil {
		return err
	}
	if native.ActualAmount <= 0 {
		return nil
	}
	var reservation billingschema.BillingReservation
	if err := tx.First(&reservation, "reservation_id = ?", native.ReservationID).Error; err != nil {
		return err
	}
	var account billingschema.BillingAccount
	if err := tx.First(&account, "account_id = ?", reservation.AccountID).Error; err != nil {
		return err
	}
	if account.AccountType != "subscription" || account.OwnerType != "user_subscription" {
		return consumeReferralSettlementCoreTx(tx, event, creditedInviters, nil)
	}
	if !strings.HasPrefix(native.IdempotencyKey, "subscription:") || !strings.Contains(native.IdempotencyKey, ":settle:") {
		return consumeReferralSettlementCoreTx(tx, event, creditedInviters, nil)
	}
	var sub commerceschema.UserSubscription
	if err := tx.First(&sub, "id = ?", account.OwnerID).Error; err != nil {
		return err
	}
	if sub.CreditPackGrantedQuota <= 0 {
		return consumeReferralSettlementCoreTx(tx, event, creditedInviters, nil)
	}
	var envelope struct {
		Context *creditPackReferralContext `json:"referral_context"`
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return err
	}
	ctx := envelope.Context
	if ctx == nil {
		return errors.New("credit pack referral route context is not ready")
	}
	if err := validateCreditPackReferralContextTx(tx, ctx, native, reservation, account, sub); err != nil {
		return err
	}
	var prior billingschema.ReferralConsumptionRebate
	if err := tx.First(&prior, "settlement_id = ?", native.SettlementID).Error; err == nil {
		if prior.FundingSource == ctx.FundingSource {
			return nil
		}
		if prior.RebateQuota > 0 {
			return errors.New("previous credit pack rebate has no matching funding receipt")
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var market *marketplaceschema.Settlement
	if ctx.FundingSource == "marketplace_channel" {
		var err error
		market, err = marketplacesettlement.LockCreditPackReferralTx(tx, ctx.Marketplace)
		if err != nil {
			return err
		}
	}
	funder := func(amount int64) error {
		if market == nil {
			return nil
		} // Proven official channel books its own cost.
		return marketplacesettlement.DebitCreditPackReferralTx(tx, market, native.SettlementID, amount)
	}
	if err := consumeReferralSettlementCoreTx(tx, event, creditedInviters, funder); err != nil {
		return err
	}
	var receipt billingschema.ReferralConsumptionRebate
	if err := tx.First(&receipt, "settlement_id = ?", native.SettlementID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		receipt = billingschema.ReferralConsumptionRebate{SettlementID: native.SettlementID, InviteeID: sub.UserId, ConsumedAt: native.SettledAt.Unix(), ExclusionReason: "referral_not_eligible"}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	updates := map[string]any{"funding_source": ctx.FundingSource}
	if market != nil {
		updates["marketplace_settlement_id"] = market.ID
	}
	if err := tx.Model(&receipt).Updates(updates).Error; err != nil {
		return err
	}
	if market != nil {
		var completed int64
		if err := tx.Model(&billingschema.ReferralConsumptionRebate{}).Where("settlement_id IN ? AND marketplace_settlement_id = ? AND funding_source = ?", ctx.SettlementIDs, market.ID, ctx.FundingSource).Count(&completed).Error; err != nil {
			return err
		}
		if completed == int64(len(ctx.SettlementIDs)) {
			return marketplacesettlement.CompleteCreditPackReferralTx(tx, market)
		}
	}
	return nil
}

func validateCreditPackReferralContextTx(tx *gorm.DB, ctx *creditPackReferralContext, native billingschema.BillingSettlement, reservation billingschema.BillingReservation, account billingschema.BillingAccount, sub commerceschema.UserSubscription) error {
	params := ctx.Marketplace
	if ctx.OriginalRequestID == "" || ctx.FundingRequestID != reservation.RequestID || ctx.AccountID != account.AccountID || ctx.ConsumerID != sub.UserId || ctx.ChannelID <= 0 || len(ctx.SettlementIDs) == 0 ||
		params.RequestID != ctx.OriginalRequestID || params.ConsumerUserID != ctx.ConsumerID || params.CreditPackGrantedQuota != sub.CreditPackGrantedQuota || params.CreditPackPaidQuota != sub.CreditPackPaidQuota || params.CreditPackChannelShareBPS != sub.CreditPackChannelShareBPS {
		return errors.New("credit pack referral context does not match native funding")
	}
	switch ctx.FundingSource {
	case "marketplace_channel":
		if params.GroupID == "" || params.OwnerUserID <= 0 || params.SettlementGrossAmount != params.ConsumerDebitAmount {
			return errors.New("invalid marketplace referral route")
		}
	case "official_channel":
		if params.GroupID != "" || params.OwnerUserID != 0 || ctx.ChannelScope != gatewayschema.ChannelScopeOfficial {
			return errors.New("official referral route lacks unambiguous channel evidence")
		}
	default:
		return errors.New("credit pack referral funding source is missing")
	}
	seen := make(map[string]bool, len(ctx.SettlementIDs))
	for _, id := range ctx.SettlementIDs {
		if id == "" || seen[id] {
			return errors.New("invalid credit pack referral settlement membership")
		}
		seen[id] = true
	}
	if !seen[native.SettlementID] {
		return errors.New("credit pack referral event is outside its funding request")
	}
	var totals struct {
		Count  int64
		Amount int64
	}
	settlements := (billingschema.BillingSettlement{}).TableName()
	reservations := (billingschema.BillingReservation{}).TableName()
	if err := tx.Table(settlements+" AS s").Select("COUNT(*) AS count, COALESCE(SUM(s.actual_amount),0) AS amount").Joins("JOIN "+reservations+" AS r ON r.reservation_id=s.reservation_id").
		Where("s.settlement_id IN ? AND r.request_id = ? AND r.account_id = ? AND s.status = ? AND r.status = ?", ctx.SettlementIDs, ctx.FundingRequestID, ctx.AccountID, billingschema.BillingSettlementStatusCompleted, billingschema.BillingReservationStatusSettled).Scan(&totals).Error; err != nil {
		return err
	}
	if totals.Count != int64(len(ctx.SettlementIDs)) || totals.Amount != params.ConsumerDebitAmount {
		return fmt.Errorf("credit pack referral native settlement sum mismatch: %d", totals.Amount)
	}
	return nil
}
