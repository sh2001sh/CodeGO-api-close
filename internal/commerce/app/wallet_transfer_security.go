package app

import (
	"errors"
	"strings"
	"time"
	"unicode"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	walletTransferMaxFailedAttempts = 5
	walletTransferLockDuration      = 30 * time.Minute
	walletTransferPasswordMinLength = 8
	walletTransferPasswordMaxLength = 64
)

type WalletTransferSecurityOverview struct {
	PasswordSet               bool   `json:"password_set"`
	LockedUntil               int64  `json:"locked_until"`
	RemainingPasswordAttempts int    `json:"remaining_password_attempts"`
	RequiresAccountPassword   bool   `json:"requires_account_password"`
	EmailBound                bool   `json:"email_bound"`
	EmailMasked               string `json:"email_masked"`
}

func GetWalletTransferSecurityOverview(userID int, hasAccountPassword bool, email string) (*WalletTransferSecurityOverview, error) {
	email = strings.TrimSpace(email)
	base := WalletTransferSecurityOverview{
		RemainingPasswordAttempts: walletTransferMaxFailedAttempts,
		RequiresAccountPassword:   hasAccountPassword,
		EmailBound:                email != "",
		EmailMasked:               maskWalletTransferEmail(email),
	}
	var security commerceschema.WalletTransferSecurity
	err := platformdb.DB.Where("user_id = ?", userID).First(&security).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &base, nil
	}
	if err != nil {
		return nil, err
	}
	base.PasswordSet = true
	base.LockedUntil = security.LockedUntil
	base.RemainingPasswordAttempts = remainingWalletTransferAttempts(security.FailedAttempts)
	return &base, nil
}

// ConfigureWalletTransferPassword creates or changes the independent payment password.
func ConfigureWalletTransferPassword(userID int, oldPassword, newPassword string) error {
	return updateWalletTransferPassword(userID, oldPassword, newPassword, true)
}

// ResetWalletTransferPassword rotates the payment password after an external
// identity check, such as a verified bound-email code.
func ResetWalletTransferPassword(userID int, newPassword string) error {
	return updateWalletTransferPassword(userID, "", newPassword, false)
}

func updateWalletTransferPassword(userID int, oldPassword, newPassword string, requireOldPassword bool) error {
	if userID <= 0 || !validWalletTransferPassword(newPassword) {
		return commerceschema.ErrWalletTransferInvalid
	}
	hash, err := platformsecurity.Password2Hash(newPassword)
	if err != nil {
		return err
	}
	var verificationErr error
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var security commerceschema.WalletTransferSecurity
		queryErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&security).Error
		if errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return tx.Create(&commerceschema.WalletTransferSecurity{UserId: userID, PasswordHash: hash}).Error
		}
		if queryErr != nil {
			return queryErr
		}
		if requireOldPassword {
			verificationErr = verifyWalletTransferPasswordLocked(tx, &security, oldPassword)
			if verificationErr != nil {
				return nil
			}
		}
		return tx.Model(&security).Updates(map[string]any{
			"password_hash":   hash,
			"failed_attempts": 0,
			"locked_until":    0,
			"updated_at":      time.Now().Unix(),
		}).Error
	})
	if err != nil {
		return err
	}
	if verificationErr != nil {
		return verificationErr
	}
	auditapp.RecordLog(userID, auditschema.LogTypeSystem, "已设置或更新额度转账支付密码")
	return nil
}

func verifyWalletTransferPassword(userID int, password string) error {
	var verificationErr error
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var security commerceschema.WalletTransferSecurity
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&security).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				verificationErr = commerceschema.ErrWalletTransferPasswordNotSet
				return nil
			}
			return err
		}
		verificationErr = verifyWalletTransferPasswordLocked(tx, &security, password)
		return nil
	})
	if err != nil {
		return err
	}
	return verificationErr
}

func verifyWalletTransferPasswordLocked(tx *gorm.DB, security *commerceschema.WalletTransferSecurity, password string) error {
	now := time.Now()
	if security.LockedUntil > now.Unix() {
		return commerceschema.ErrWalletTransferPasswordLocked
	}
	if platformsecurity.ValidatePasswordAndHash(password, security.PasswordHash) {
		if security.FailedAttempts == 0 && security.LockedUntil == 0 {
			return nil
		}
		return tx.Model(security).Updates(map[string]any{"failed_attempts": 0, "locked_until": 0, "updated_at": now.Unix()}).Error
	}

	failedAttempts := security.FailedAttempts + 1
	lockedUntil := int64(0)
	if failedAttempts >= walletTransferMaxFailedAttempts {
		lockedUntil = now.Add(walletTransferLockDuration).Unix()
		failedAttempts = 0
	}
	if err := tx.Model(security).Updates(map[string]any{
		"failed_attempts": failedAttempts,
		"locked_until":    lockedUntil,
		"updated_at":      now.Unix(),
	}).Error; err != nil {
		return err
	}
	if lockedUntil > 0 {
		if err := auditapp.RecordLogTx(tx, security.UserId, auditschema.LogTypeSystem, "额度转账支付密码连续输错，已锁定 30 分钟"); err != nil {
			return err
		}
		return commerceschema.ErrWalletTransferPasswordLocked
	}
	return commerceschema.ErrWalletTransferPasswordIncorrect
}

func validWalletTransferPassword(password string) bool {
	if password != strings.TrimSpace(password) {
		return false
	}
	runes := []rune(password)
	if len(runes) < walletTransferPasswordMinLength || len(runes) > walletTransferPasswordMaxLength {
		return false
	}
	var hasLetter, hasNumber bool
	for _, value := range runes {
		hasLetter = hasLetter || unicode.IsLetter(value)
		hasNumber = hasNumber || unicode.IsNumber(value)
	}
	return hasLetter && hasNumber
}

func remainingWalletTransferAttempts(failedAttempts int) int {
	remaining := walletTransferMaxFailedAttempts - failedAttempts
	if remaining < 0 {
		return 0
	}
	return remaining
}

func maskWalletTransferEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return ""
	}
	local := []rune(parts[0])
	visible := string(local[0])
	if len(local) > 2 {
		visible += string(local[len(local)-1])
	}
	return visible + "***@" + parts[1]
}
