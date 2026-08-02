package app

import (
	"errors"
	"fmt"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"gorm.io/gorm"
)

// DebitWalletQuotaTx atomically debits the standard wallet and its ledger mirror.
func DebitWalletQuotaTx(tx *gorm.DB, userID int, amount int, operationID string) error {
	return debitUserWalletQuotaTx(tx, userID, amount, operationID, mirroredWalletTxStore{
		accountType: billingAccountTypeWallet,
		readBalance: getUserWalletQuotaTx,
		applyDelta:  decreaseUserWalletQuotaTx,
	}, true)
}

// DebitClaudeWalletQuotaTx atomically debits the Claude wallet and its ledger mirror.
func DebitClaudeWalletQuotaTx(tx *gorm.DB, userID int, amount int, operationID string) error {
	return debitUserWalletQuotaTx(tx, userID, amount, operationID, mirroredWalletTxStore{
		accountType: billingAccountTypeClaudeWallet,
		readBalance: getUserClaudeWalletQuotaTx,
		applyDelta:  decreaseUserClaudeWalletQuotaTx,
	}, false)
}

func debitUserWalletQuotaTx(tx *gorm.DB, userID int, amount int, operationID string, mirrored mirroredWalletTxStore, consumeBonus bool) error {
	if tx == nil {
		return errors.New("transaction is required")
	}
	if userID <= 0 || amount <= 0 || operationID == "" {
		return errors.New("invalid wallet debit")
	}

	legacyBalance, err := mirrored.readBalance(tx, userID)
	if err != nil {
		return err
	}
	if legacyBalance < amount {
		return billingdomain.ErrInsufficientBalance
	}
	account, err := ensureMirroredUserAccountTx(tx, userID, mirrored.accountType, legacyBalance)
	if err != nil {
		return err
	}
	if err := reconcileAccountBalanceTx(tx, account, userID, legacyBalance); err != nil {
		return err
	}
	if err := applyLedgerDeltaTx(tx, account, userID, amount, operationID, "wallet_quota_conversion_debit"); err != nil {
		return err
	}
	if err := mirrored.applyDelta(tx, userID, amount); err != nil {
		return err
	}
	if consumeBonus {
		return ConsumeBonusWalletQuotaCreditsTx(tx, userID, int64(amount))
	}
	return nil
}

func decreaseUserWalletQuotaTx(tx *gorm.DB, userID int, amount int) error {
	return decreaseUserQuotaColumnTx(tx, userID, "quota", amount)
}

func decreaseUserClaudeWalletQuotaTx(tx *gorm.DB, userID int, amount int) error {
	return decreaseUserQuotaColumnTx(tx, userID, "claude_quota", amount)
}

func decreaseUserQuotaColumnTx(tx *gorm.DB, userID int, column string, amount int) error {
	result := tx.Model(&identityschema.User{}).
		Where(fmt.Sprintf("id = ? AND %s >= ?", column), userID, amount).
		UpdateColumn(column, gorm.Expr(column+" - ?", amount))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return billingdomain.ErrInsufficientBalance
	}
	return nil
}
