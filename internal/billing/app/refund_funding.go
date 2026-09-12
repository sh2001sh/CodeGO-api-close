package app

import (
	"errors"
	"fmt"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TopupFundingLotRemaining returns the quota from one paid top-up that has not
// been consumed. Bonus, transfer, and administrator-granted lots are excluded.
func TopupFundingLotRemaining(tx *gorm.DB, userID int, tradeNo string) (int64, error) {
	if tx == nil || userID <= 0 || strings.TrimSpace(tradeNo) == "" {
		return 0, errors.New("invalid top-up funding lookup")
	}
	if !tx.Migrator().HasTable(&billingschema.FundingLot{}) {
		return 0, errors.New("funding attribution is not available")
	}
	account, err := findUserClaudeWalletAccountTx(tx, userID)
	if err != nil {
		return 0, err
	}
	var lot billingschema.FundingLot
	key := fmt.Sprintf("topup:%s:unified", strings.TrimSpace(tradeNo))
	if err := tx.Where("account_id = ? AND idempotency_key = ?", account.AccountID, key).First(&lot).Error; err != nil {
		return 0, err
	}
	if lot.Source != billingschema.FundingSourceTopup || lot.RemainingAmount <= 0 {
		return 0, nil
	}
	return lot.RemainingAmount, nil
}

// RefundTopupFundingLotTx removes exactly one paid top-up's remaining quota
// from the wallet and closes its funding lot. It deliberately skips reward
// holds because blind-box rewards are not refundable.
func RefundTopupFundingLotTx(tx *gorm.DB, userID int, tradeNo string, amount int64, operationID string) error {
	if tx == nil || userID <= 0 || strings.TrimSpace(tradeNo) == "" || amount <= 0 || strings.TrimSpace(operationID) == "" {
		return errors.New("invalid top-up refund")
	}
	account, err := findUserClaudeWalletAccountTx(tx, userID)
	if err != nil {
		return err
	}
	var lot billingschema.FundingLot
	key := fmt.Sprintf("topup:%s:unified", strings.TrimSpace(tradeNo))
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ? AND idempotency_key = ?", account.AccountID, key).First(&lot).Error; err != nil {
		return err
	}
	if lot.Source != billingschema.FundingSourceTopup || lot.RemainingAmount != amount {
		return errors.New("top-up refundable quota changed")
	}
	if err := DebitClaudeWalletQuotaTxWithReason(tx, userID, int(amount), operationID, "payment_refund"); err != nil {
		return err
	}
	return tx.Model(&lot).Update("remaining_amount", 0).Error
}
