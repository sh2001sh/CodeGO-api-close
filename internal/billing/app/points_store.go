package app

import (
	"errors"
	"strings"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnsurePointAccountTx loads the user's point account with a row lock, creating it if needed.
func EnsurePointAccountTx(tx *gorm.DB, userID int) (*billingschema.PointAccount, error) {
	if tx == nil {
		tx = platformdb.DB
	}
	if userID <= 0 {
		return nil, errors.New("invalid point account user id")
	}

	var account billingschema.PointAccount
	err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&billingschema.PointAccount{UserId: userID}).Error
	if err != nil {
		return nil, err
	}
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// AddPointLedgerTx applies a point ledger delta and persists the resulting ledger entry.
func AddPointLedgerTx(tx *gorm.DB, userID int, ledgerType string, delta int64, sourceType string, sourceID string, key string, note string) (*billingschema.PointLedger, bool, error) {
	if tx == nil {
		tx = platformdb.DB
	}
	if userID <= 0 || key == "" || delta == 0 {
		return nil, false, errors.New("invalid point ledger")
	}

	var existing billingschema.PointLedger
	if err := tx.Where("idempotency_key = ?", key).First(&existing).Error; err == nil {
		return &existing, false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	account, err := EnsurePointAccountTx(tx, userID)
	if err != nil {
		return nil, false, err
	}
	switch ledgerType {
	case billingschema.PointLedgerTypeEarn, billingschema.PointLedgerTypeRefund:
		account.Balance += delta
	case billingschema.PointLedgerTypeSpend:
		if account.Balance+delta < 0 {
			return nil, false, errors.New("points balance is insufficient")
		}
		account.Balance += delta
	case billingschema.PointLedgerTypeFreeze:
		account.FrozenBalance += delta
	case billingschema.PointLedgerTypeRelease:
		if account.FrozenBalance+delta < 0 {
			return nil, false, errors.New("frozen points balance is insufficient")
		}
		account.FrozenBalance += delta
		account.Balance -= delta
	default:
		return nil, false, errors.New("invalid point ledger type")
	}
	if err := tx.Save(account).Error; err != nil {
		return nil, false, err
	}

	ledger := &billingschema.PointLedger{
		UserId:         userID,
		Type:           ledgerType,
		Delta:          delta,
		BalanceAfter:   account.Balance,
		FrozenAfter:    account.FrozenBalance,
		SourceType:     sourceType,
		SourceId:       sourceID,
		IdempotencyKey: key,
		Note:           strings.TrimSpace(note),
	}
	if err := tx.Create(ledger).Error; err != nil {
		return nil, false, err
	}
	return ledger, true, nil
}
