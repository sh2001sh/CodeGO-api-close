package settlement

import (
	"errors"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LockCreditPackReferralTx recreates a late projection and holds the same
// settlement and pending-balance locks as owner release until referral debits
// and receipts have committed.
func LockCreditPackReferralTx(tx *gorm.DB, params RecordParams) (*marketplaceschema.Settlement, error) {
	if params.RequestID == "" || params.GroupID == "" || params.OwnerUserID <= 0 || params.ConsumerUserID <= 0 || params.CreditPackGrantedQuota <= 0 {
		return nil, errors.New("invalid marketplace credit pack referral context")
	}
	if err := RecordTx(tx, params); err != nil {
		return nil, err
	}
	var item marketplaceschema.Settlement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "request_id = ?", params.RequestID).Error; err != nil {
		return nil, err
	}
	if item.GroupID != params.GroupID || item.OwnerUserID != params.OwnerUserID || item.ConsumerUserID != params.ConsumerUserID ||
		item.ConsumerAmount != params.ConsumerDebitAmount || item.SettlementGrossAmount != params.SettlementGrossAmount ||
		item.CreditPackGrantedQuota != params.CreditPackGrantedQuota || item.CreditPackPaidQuota != params.CreditPackPaidQuota || item.CreditPackChannelShareBPS != params.CreditPackChannelShareBPS {
		return nil, errors.New("marketplace referral settlement snapshot mismatch")
	}
	if item.Status != statusPending {
		return nil, errors.New("marketplace referral funding is no longer pending")
	}
	// Owner release locks settlement -> pending balance -> user wallet. Take the
	// pending balance here before referral credits any inviter wallet so both
	// workflows use the same global order.
	if err := billingdomain.LockBalanceSnapshotTx(tx, item.PendingAccountID); err != nil {
		return nil, err
	}
	return &item, nil
}

// DebitCreditPackReferralTx moves the exact reward out of owner earnings; it
// does not credit platform revenue. The caller credits the inviter atomically.
func DebitCreditPackReferralTx(tx *gorm.DB, item *marketplaceschema.Settlement, financialSettlementID string, amount int64) error {
	if amount < 0 || item == nil || financialSettlementID == "" {
		return errors.New("invalid marketplace referral debit")
	}
	if amount == 0 {
		return nil
	}
	if !item.ReferralRebatePending || item.Status != statusPending || item.OwnerNetAmount < amount {
		return errors.New("marketplace owner referral funds are unavailable")
	}
	key := "marketplace-referral:" + financialSettlementID
	var existing billingschema.BillingSettlement
	if err := tx.First(&existing, "idempotency_key = ?", key+":settle").Error; err == nil {
		if existing.ActualAmount != amount {
			return errors.New("marketplace referral debit replay amount mismatch")
		}
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{AccountID: item.PendingAccountID, RequestID: key, ReservedAmount: amount, IdempotencyKey: key + ":reserve"})
	if err != nil {
		return err
	}
	if _, err = billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: amount, IdempotencyKey: key + ":settle"}); err != nil {
		return err
	}
	item.OwnerNetAmount -= amount
	item.ReferralRebateAmount += amount
	if item.SettlementGrossAmount-item.PlatformCommission-item.TransactionFee-item.ReferralRebateAmount != item.OwnerNetAmount {
		return errors.New("marketplace referral settlement conservation mismatch")
	}
	return tx.Model(item).Updates(map[string]any{"owner_net_amount": item.OwnerNetAmount, "referral_rebate_amount": item.ReferralRebateAmount}).Error
}

func CompleteCreditPackReferralTx(tx *gorm.DB, item *marketplaceschema.Settlement) error {
	if item == nil || item.Status != statusPending {
		return errors.New("marketplace referral settlement is unavailable")
	}
	return tx.Model(item).Update("referral_rebate_pending", false).Error
}
