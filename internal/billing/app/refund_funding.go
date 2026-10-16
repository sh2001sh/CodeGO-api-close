package app

import (
	"errors"
	"fmt"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TopupFundingLotAmounts returns the original and remaining quota from one paid
// top-up. Bonus, transfer, and administrator-granted lots are excluded.
func TopupFundingLotAmounts(tx *gorm.DB, userID int, tradeNo string) (int64, int64, error) {
	if tx == nil || userID <= 0 || strings.TrimSpace(tradeNo) == "" {
		return 0, 0, errors.New("invalid top-up funding lookup")
	}
	if !tx.Migrator().HasTable(&billingschema.FundingLot{}) {
		return 0, 0, errors.New("funding attribution is not available")
	}
	account, err := findUserClaudeWalletAccountTx(tx, userID)
	if err != nil {
		return 0, 0, err
	}
	var lot billingschema.FundingLot
	key := fmt.Sprintf("topup:%s:unified", strings.TrimSpace(tradeNo))
	if err := tx.Where("account_id = ? AND idempotency_key = ?", account.AccountID, key).First(&lot).Error; err != nil {
		return 0, 0, err
	}
	if lot.Source != billingschema.FundingSourceTopup || lot.OriginalAmount <= 0 {
		return 0, 0, nil
	}
	return lot.OriginalAmount, max(lot.RemainingAmount, 0), nil
}

// ReserveTopupRefundTx freezes both spendable projections and the exact paid
// funding lot before any irreversible provider request. An empty RequestID
// keeps the payment hold outside the relay stale-request recovery worker.
func ReserveTopupRefundTx(tx *gorm.DB, userID int, tradeNo string, amount int64, refundNo string) error {
	if tx == nil || userID <= 0 || amount <= 0 || strings.TrimSpace(tradeNo) == "" || strings.TrimSpace(refundNo) == "" {
		return errors.New("invalid top-up refund reservation")
	}
	account, err := findUserClaudeWalletAccountTx(tx, userID)
	if err != nil {
		return err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return err
	}
	var user identityschema.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "claude_quota").Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}
	if snapshot.ReservedBalance != 0 || snapshot.AvailableBalance != int64(user.ClaudeQuota) || snapshot.AvailableBalance < amount {
		return errors.New("wallet has pending usage or inconsistent refundable balance")
	}
	// Older transfers did not consume lots. Do not refund a stale paid lot
	// against replacement gifts or rewards until attribution is reconciled.
	var attributed int64
	if err := tx.Model(&billingschema.FundingLot{}).Where("account_id = ? AND remaining_amount > 0", account.AccountID).Select("coalesce(sum(remaining_amount),0)").Scan(&attributed).Error; err != nil {
		return err
	}
	if attributed > snapshot.AvailableBalance {
		return errors.New("wallet funding attribution requires review")
	}
	var lot billingschema.FundingLot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ? AND idempotency_key = ?", account.AccountID, "topup:"+strings.TrimSpace(tradeNo)+":unified").First(&lot).Error; err != nil {
		return err
	}
	if lot.Source != billingschema.FundingSourceTopup || lot.RemainingAmount != amount || lot.OriginalAmount < amount {
		return errors.New("top-up refundable quota changed")
	}
	if _, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{AccountID: account.AccountID, WorkflowID: refundNo, ReservedAmount: amount, IdempotencyKey: "payment-refund:" + refundNo + ":reserve"}); err != nil {
		return err
	}
	if err := decreaseUserClaudeWalletQuotaTx(tx, userID, amount); err != nil {
		return err
	}
	return tx.Model(&lot).Update("remaining_amount", 0).Error
}

// CompleteTopupRefundTx settles the durable hold or releases it back to its
// original lot. It never credits a new lot or calls the payment provider.
func CompleteTopupRefundTx(tx *gorm.DB, userID int, tradeNo string, amount int64, refundNo string, success bool) error {
	if tx == nil || userID <= 0 || amount <= 0 || strings.TrimSpace(tradeNo) == "" || strings.TrimSpace(refundNo) == "" {
		return errors.New("invalid top-up refund completion")
	}
	account, err := findUserClaudeWalletAccountTx(tx, userID)
	if err != nil {
		return err
	}
	var reservation billingschema.BillingReservation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", "payment-refund:"+refundNo+":reserve").First(&reservation).Error; err != nil {
		return err
	}
	if reservation.AccountID != account.AccountID || reservation.ReservedAmount != amount || reservation.WorkflowID != refundNo || reservation.RequestID != "" {
		return errors.New("refund reservation mismatch")
	}
	if success {
		_, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: amount, IdempotencyKey: "payment-refund:" + refundNo + ":settle"})
		return err
	}
	if reservation.Status == billingschema.BillingReservationStatusReleased {
		return nil
	}
	if reservation.Status != billingschema.BillingReservationStatusOpen {
		return errors.New("refund reservation is terminal")
	}
	if _, err := billingdomain.ReleaseReservationTx(tx, billingdomain.ReleaseReservationParams{ReservationID: reservation.ReservationID, IdempotencyKey: "payment-refund:" + refundNo + ":release", ReasonCode: "payment_refund_failed"}); err != nil {
		return err
	}
	var lot billingschema.FundingLot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ? AND idempotency_key = ?", account.AccountID, "topup:"+strings.TrimSpace(tradeNo)+":unified").First(&lot).Error; err != nil {
		return err
	}
	if lot.Source != billingschema.FundingSourceTopup || lot.RemainingAmount != 0 || lot.OriginalAmount < amount {
		return errors.New("refund funding lot mismatch")
	}
	if err := tx.Model(&lot).Update("remaining_amount", amount).Error; err != nil {
		return err
	}
	return increaseUserClaudeWalletQuotaTx(tx, userID, amount)
}
