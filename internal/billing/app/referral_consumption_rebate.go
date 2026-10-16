package app

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const referralConsumptionWindowSeconds int64 = 7 * 24 * 60 * 60

// SnapshotTopupPaidQuotaTx freezes the actual paid portion of a credited wallet
// lot in the payment transaction. Policy multipliers are deliberately unrelated.
func SnapshotTopupPaidQuotaTx(tx *gorm.DB, tradeNo string, paidQuota int64) error {
	if tx == nil || tradeNo == "" || paidQuota <= 0 {
		return errors.New("invalid paid topup quota snapshot")
	}
	var lot billingschema.FundingLot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", "topup:"+tradeNo+":unified").First(&lot).Error; err != nil {
		return err
	}
	if lot.Source != billingschema.FundingSourceTopup || lot.OriginalAmount <= 0 || paidQuota > lot.OriginalAmount || (lot.PaidQuota > 0 && lot.PaidQuota != paidQuota) {
		return errors.New("paid topup quota snapshot conflict")
	}
	return tx.Model(&lot).Update("paid_quota", paidQuota).Error
}

// consumeReferralSettlementTx runs only in the asynchronous ledger outbox
// transaction. A failed wallet transfer leaves its source event pending.
func consumeReferralSettlementCoreTx(tx *gorm.DB, event billingschema.BillingOutboxEvent, creditedInviters map[int]struct{}, fundRebate func(int64) error) error {
	if event.EventType != "billing.reservation_settled" {
		return nil
	}
	var settlement billingschema.BillingSettlement
	if err := tx.First(&settlement, "settlement_id = ?", event.AggregateID).Error; err != nil {
		return err
	}
	if settlement.Status != billingschema.BillingSettlementStatusCompleted || settlement.ActualAmount <= 0 {
		return nil
	}
	var reservation billingschema.BillingReservation
	if err := tx.First(&reservation, "reservation_id = ?", settlement.ReservationID).Error; err != nil {
		return err
	}
	if reservation.Status != billingschema.BillingReservationStatusSettled {
		return nil
	}
	var account billingschema.BillingAccount
	if err := tx.First(&account, "account_id = ?", reservation.AccountID).Error; err != nil {
		return err
	}
	var inviteeID int
	var subscription commerceschema.UserSubscription
	switch {
	case account.OwnerType == "user" && (account.AccountType == billingAccountTypeClaudeWallet || account.AccountType == billingAccountTypeWallet) && strings.HasPrefix(settlement.IdempotencyKey, "relay:wallet:") && strings.HasSuffix(settlement.IdempotencyKey, ":settle"):
		inviteeID = int(account.OwnerID)
	case account.OwnerType == "user_subscription" && account.AccountType == "subscription" && strings.HasPrefix(settlement.IdempotencyKey, "subscription:") && strings.Contains(settlement.IdempotencyKey, ":settle:"):
		if err := tx.First(&subscription, "id = ?", account.OwnerID).Error; err != nil {
			return err
		}
		inviteeID = subscription.UserId
	default:
		return nil // Transfers, marketplace release, and purchases are not relay use.
	}
	var invitee identityschema.User
	if err := tx.First(&invitee, "id = ?", inviteeID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	consumedAt := settlement.SettledAt.Unix()
	if invitee.InviterId <= 0 || invitee.InviterId == invitee.Id || invitee.CreatedAt <= 0 || consumedAt < invitee.CreatedAt || consumedAt-invitee.CreatedAt >= referralConsumptionWindowSeconds {
		return nil
	}
	var program billingschema.ReferralRebateProgram
	if err := tx.First(&program, "id = ?", 1).Error; err != nil {
		return fmt.Errorf("load referral rebate program: %w", err)
	}
	if program.RateBPS <= 0 || program.RateBPS > 10000 || program.EffectiveAt <= 0 {
		return errors.New("invalid referral rebate program")
	}
	if consumedAt < program.EffectiveAt {
		return nil
	}
	// Sorting both identities prevents reciprocal invite relationships from
	// taking opposite user locks. The invitee lock serializes cumulative rounding.
	ids := []int{invitee.Id, invitee.InviterId}
	sort.Ints(ids)
	var lockedUsers []identityschema.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id asc").Find(&lockedUsers).Error; err != nil {
		return err
	}
	if len(lockedUsers) != 2 {
		return nil
	}
	for _, user := range lockedUsers {
		if user.Id == invitee.Id && user.InviterId != invitee.InviterId {
			return errors.New("referral ownership changed while processing settlement")
		}
	}
	var existing billingschema.ReferralConsumptionRebate
	if err := tx.First(&existing, "settlement_id = ?", settlement.SettlementID).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	eligible, reason, err := referralEligibleQuotaTx(tx, settlement, reservation, account, subscription)
	if err != nil {
		return err
	}
	var totals struct {
		Eligible int64
		Rebate   int64
	}
	if err := tx.Model(&billingschema.ReferralConsumptionRebate{}).
		Where("invitee_id = ? AND inviter_id = ? AND rate_bps = ?", invitee.Id, invitee.InviterId, program.RateBPS).
		Select("COALESCE(SUM(eligible_quota), 0) AS eligible, COALESCE(SUM(rebate_quota), 0) AS rebate").Scan(&totals).Error; err != nil {
		return err
	}
	base := new(big.Int).Add(big.NewInt(totals.Eligible), big.NewInt(eligible))
	base.Mul(base, big.NewInt(program.RateBPS)).Quo(base, big.NewInt(10000)).Sub(base, big.NewInt(totals.Rebate))
	if !base.IsInt64() || base.Sign() < 0 {
		return errors.New("referral rebate cumulative amount is invalid")
	}
	rebate := base.Int64()
	receipt := billingschema.ReferralConsumptionRebate{
		SettlementID: settlement.SettlementID, InviterID: invitee.InviterId, InviteeID: invitee.Id,
		EligibleQuota: eligible, RebateQuota: rebate, RateBPS: program.RateBPS, ConsumedAt: consumedAt, ExclusionReason: reason,
	}
	if err := tx.Create(&receipt).Error; err != nil {
		return err
	}
	if rebate > 0 {
		if fundRebate != nil {
			if err := fundRebate(rebate); err != nil {
				return err
			}
		}
		if err := CreditMarketplaceOwnerEarningsTx(tx, invitee.InviterId, rebate, "referral-consumption:"+settlement.SettlementID, "referral_consumption_rebate"); err != nil {
			return err
		}
		creditedInviters[invitee.InviterId] = struct{}{}
	}
	return nil
}

func referralEligibleQuotaTx(tx *gorm.DB, settlement billingschema.BillingSettlement, reservation billingschema.BillingReservation, account billingschema.BillingAccount, sub commerceschema.UserSubscription) (int64, string, error) {
	if account.OwnerType == "user_subscription" {
		if sub.CreditPackGrantedQuota <= 0 || sub.CreditPackPaidQuota <= 0 || sub.CreditPackPaidQuota > sub.CreditPackGrantedQuota {
			return 0, "legacy_paid_snapshot_missing", nil
		}
		var order commerceschema.SubscriptionOrder
		err := tx.Where("target_subscription_id = ? AND user_id = ? AND status = ? AND credit_pack_quota = ? AND credit_pack_paid_quota = ? AND money > 0", sub.Id, sub.UserId, constant.TopUpStatusSuccess, sub.CreditPackGrantedQuota, sub.CreditPackPaidQuota).First(&order).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, "conversion_paid_snapshot_missing", nil
		}
		if err != nil {
			return 0, "", err
		}
		// Carry the paid-ratio fraction across this immutable package's requests.
		var prior struct{ Gross int64 }
		receipts := (billingschema.ReferralConsumptionRebate{}).TableName()
		settlements := (billingschema.BillingSettlement{}).TableName()
		reservations := (billingschema.BillingReservation{}).TableName()
		if err := tx.Table(receipts+" AS rebate").Select("COALESCE(SUM(settled.actual_amount), 0) AS gross").
			Joins("JOIN "+settlements+" AS settled ON settled.settlement_id = rebate.settlement_id").
			Joins("JOIN "+reservations+" AS reserved ON reserved.reservation_id = settled.reservation_id").
			Where("reserved.account_id = ?", account.AccountID).Scan(&prior).Error; err != nil {
			return 0, "", err
		}
		if prior.Gross < 0 || prior.Gross > sub.CreditPackGrantedQuota-settlement.ActualAmount {
			return 0, "", errors.New("referral credit pack consumed amount exceeds purchase snapshot")
		}
		eligible := floorPaidQuota(prior.Gross+settlement.ActualAmount, sub.CreditPackPaidQuota, sub.CreditPackGrantedQuota) - floorPaidQuota(prior.Gross, sub.CreditPackPaidQuota, sub.CreditPackGrantedQuota)
		if eligible < 0 {
			return 0, "", errors.New("invalid cumulative credit pack referral attribution")
		}
		return eligible, "", nil
	}
	if err := allocateSettledFundingFIFOTx(tx, reservation.RequestID, account.AccountID, settlement.ActualAmount); err != nil {
		return 0, "", err
	}
	var allocations []billingschema.FundingAllocation
	if err := tx.Where("request_id = ? AND account_id = ?", reservation.RequestID, account.AccountID).Find(&allocations).Error; err != nil {
		return 0, "", err
	}
	var allocated, eligible int64
	reason := "unpaid_funding"
	for _, allocation := range allocations {
		if allocation.Amount <= 0 || allocation.Amount > settlement.ActualAmount-allocated {
			return 0, "", errors.New("referral funding allocations do not match settled amount")
		}
		allocated += allocation.Amount
		var lot billingschema.FundingLot
		if err := tx.First(&lot, "lot_id = ? AND account_id = ?", allocation.LotID, account.AccountID).Error; err != nil {
			return 0, "", err
		}
		if lot.Source != billingschema.FundingSourceTopup || allocation.Source != lot.Source {
			continue
		}
		if lot.PaidQuota <= 0 || lot.PaidQuota > lot.OriginalAmount {
			reason = "legacy_paid_snapshot_missing"
			continue
		}
		trade := strings.TrimSuffix(strings.TrimPrefix(lot.IdempotencyKey, "topup:"), ":unified")
		if "topup:"+trade+":unified" != lot.IdempotencyKey || trade == "" {
			reason = "paid_order_missing"
			continue
		}
		var order struct {
			UserID    int
			PaidQuota int64
			Status    string
			Money     float64
		}
		if err := tx.Model(&commerceschema.TopUp{}).Select("user_id, paid_quota, status, money").Where("trade_no = ?", trade).Take(&order).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			reason = "paid_order_missing"
			continue
		} else if err != nil {
			return 0, "", err
		}
		if order.UserID != int(account.OwnerID) || order.Status != constant.TopUpStatusSuccess || order.Money <= 0 || order.PaidQuota != lot.PaidQuota {
			reason = "paid_order_snapshot_mismatch"
			continue
		}
		if allocation.PaidQuota < 0 || allocation.PaidQuota > allocation.Amount {
			return 0, "", errors.New("invalid paid funding allocation snapshot")
		}
		eligible += allocation.PaidQuota
	}
	if allocated != settlement.ActualAmount {
		return 0, "", fmt.Errorf("referral funding allocation incomplete: %d of %d", allocated, settlement.ActualAmount)
	}
	if eligible > 0 {
		reason = ""
	}
	return eligible, reason, nil
}

func floorPaidQuota(amount, paid, granted int64) int64 {
	value := new(big.Int).Mul(big.NewInt(amount), big.NewInt(paid))
	return value.Quo(value, big.NewInt(granted)).Int64()
}
