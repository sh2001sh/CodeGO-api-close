package app

import (
	"errors"
	"math"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	walletRewardTransferReleaseStart = 24 * time.Hour
	walletRewardTransferReleaseFull  = 72 * time.Hour
)

var ErrWalletRewardTransferLocked = errors.New("盲盒奖励仍在释放期，暂不可转出")

// CreateWalletRewardHoldTx records a blind-box reward hold for a newly created
// account. The reward remains in the normal wallet and can be spent by its owner.
func CreateWalletRewardHoldTx(tx *gorm.DB, userID int, amount int64, idempotencyKey string) error {
	if tx == nil || userID <= 0 || amount <= 0 || idempotencyKey == "" {
		return errors.New("invalid wallet reward hold")
	}
	if !tx.Migrator().HasTable(&billingschema.WalletRewardHold{}) {
		return nil
	}
	var user identityschema.User
	if err := tx.Select("id, created_at").First(&user, userID).Error; err != nil {
		return err
	}
	if !isNewWalletRewardAccount(user.CreatedAt, time.Now()) {
		return nil
	}
	var account billingschema.BillingAccount
	if err := tx.Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?",
		billingOwnerTypeUser, userID, billingAccountTypeClaudeWallet, billingQuotaUnitQuota,
	).First(&account).Error; err != nil {
		return err
	}
	var existing billingschema.WalletRewardHold
	lookup := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing)
	if lookup.Error == nil {
		if existing.AccountID != account.AccountID || existing.OriginalAmount != amount {
			return errors.New("wallet reward hold idempotency conflict")
		}
		return nil
	}
	if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return lookup.Error
	}
	return tx.Create(&billingschema.WalletRewardHold{
		AccountID: account.AccountID, UserID: userID, OriginalAmount: amount,
		ReferenceType: "blind_box_reward", ReferenceID: idempotencyKey,
		IdempotencyKey: idempotencyKey,
	}).Error
}

// EnsureWalletTransferQuotaTx rejects transfers that would spend unreleased
// blind-box rewards. It intentionally does not affect owner-only consumption.
func EnsureWalletTransferQuotaTx(tx *gorm.DB, userID int, amount int64) error {
	if tx == nil || userID <= 0 || amount <= 0 {
		return errors.New("invalid wallet transfer quota")
	}
	balance, err := GetUnifiedCreditBalanceTx(tx, userID)
	if err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&billingschema.WalletRewardHold{}) {
		if int64(balance) < amount {
			return billingdomain.ErrInsufficientBalance
		}
		return nil
	}
	account, err := findUserClaudeWalletAccountTx(tx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if int64(balance) < amount {
			return billingdomain.ErrInsufficientBalance
		}
		return nil
	}
	if err != nil {
		return err
	}
	var user identityschema.User
	if err := tx.Select("id, created_at").First(&user, userID).Error; err != nil {
		return err
	}
	var holds []billingschema.WalletRewardHold
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND consumed_amount < original_amount", account.AccountID).
		Find(&holds).Error; err != nil {
		return err
	}
	locked := int64(0)
	for _, hold := range holds {
		locked += unreleasedWalletRewardAmount(hold.OriginalAmount, hold.ConsumedAmount, user.CreatedAt, time.Now())
	}
	transferable := int64(balance) - locked
	if transferable < amount {
		return ErrWalletRewardTransferLocked
	}
	return nil
}

// ConsumeWalletRewardHoldsTx consumes held rewards before unlocked balance when
// the owner uses quota. Peer-transfer debits deliberately skip this function.
func ConsumeWalletRewardHoldsTx(tx *gorm.DB, accountID string, amount int64) error {
	if tx == nil || accountID == "" || amount <= 0 {
		return nil
	}
	if !tx.Migrator().HasTable(&billingschema.WalletRewardHold{}) {
		return nil
	}
	var holds []billingschema.WalletRewardHold
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND consumed_amount < original_amount", accountID).
		Order("created_at asc, hold_id asc").Find(&holds).Error; err != nil {
		return err
	}
	remaining := amount
	for index := range holds {
		available := holds[index].OriginalAmount - holds[index].ConsumedAmount
		if available <= 0 {
			continue
		}
		consumed := available
		if consumed > remaining {
			consumed = remaining
		}
		holds[index].ConsumedAmount += consumed
		if err := tx.Model(&holds[index]).Update("consumed_amount", holds[index].ConsumedAmount).Error; err != nil {
			return err
		}
		remaining -= consumed
		if remaining == 0 {
			break
		}
	}
	return nil
}

func findUserClaudeWalletAccountTx(tx *gorm.DB, userID int) (*billingschema.BillingAccount, error) {
	var account billingschema.BillingAccount
	err := tx.Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?",
		billingOwnerTypeUser, userID, billingAccountTypeClaudeWallet, billingQuotaUnitQuota,
	).First(&account).Error
	return &account, err
}

func isNewWalletRewardAccount(createdAt int64, now time.Time) bool {
	if createdAt <= 0 {
		return false
	}
	age := now.Sub(time.Unix(createdAt, 0))
	return age >= 0 && age < walletRewardTransferReleaseFull
}

func unreleasedWalletRewardAmount(original, consumed int64, createdAt int64, now time.Time) int64 {
	if original <= 0 {
		return 0
	}
	released := float64(original) * walletRewardReleaseRatio(createdAt, now)
	locked := original - consumed - int64(math.Floor(released))
	if locked < 0 {
		return 0
	}
	return locked
}

func walletRewardReleaseRatio(createdAt int64, now time.Time) float64 {
	if createdAt <= 0 {
		return 1
	}
	age := now.Sub(time.Unix(createdAt, 0))
	if age < walletRewardTransferReleaseStart {
		return 0
	}
	if age >= walletRewardTransferReleaseFull {
		return 1
	}
	ratio := float64(age-walletRewardTransferReleaseStart) / float64(walletRewardTransferReleaseFull-walletRewardTransferReleaseStart)
	if ratio < 0 {
		return 0
	}
	return math.Min(1, ratio)
}
