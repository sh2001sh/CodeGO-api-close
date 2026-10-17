package store

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

var ErrEmailConflict = errors.New("邮箱地址已被占用或存在冲突")

func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// PostgreSQL has no unique email constraint while legacy duplicates are retained.
// Every new nonempty binding takes this transaction-scoped lock before checking
// ownership. SQLite serializes writes; other dialects still need a DB constraint.
func lockEmailOwnership(tx *gorm.DB, email string) error {
	if tx.Dialector.Name() != "postgres" || NormalizeEmail(email) == "" {
		return nil
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errors.New("email ownership requires a transaction")
	}
	var isolation string
	if err := tx.Raw("SHOW transaction_isolation").Scan(&isolation).Error; err != nil {
		return err
	}
	if isolation != "read committed" {
		return errors.New("email ownership requires read committed isolation")
	}
	digest := sha256.Sum256([]byte("identity:normalized-email:" + NormalizeEmail(email)))
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(binary.BigEndian.Uint64(digest[:8]))).Error
}

func IsEmailTaken(email string) bool {
	var count int64
	err := platformdb.DB.Unscoped().Model(&identityschema.User{}).Where("LOWER(TRIM(email)) = ?", NormalizeEmail(email)).Count(&count).Error
	return err != nil || count > 0
}

// UniqueUserByEmail refuses ambiguous legacy bindings without changing them.
func UniqueUserByEmail(email string) (*identityschema.User, error) {
	return uniqueUserByEmail(platformdb.DB, email)
}

func uniqueUserByEmail(db *gorm.DB, email string) (*identityschema.User, error) {
	if NormalizeEmail(email) == "" {
		return nil, ErrEmailConflict
	}
	var users []identityschema.User
	if err := db.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("LOWER(TRIM(email)) = ?", NormalizeEmail(email)).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) != 1 || users[0].DeletedAt.Valid {
		return nil, ErrEmailConflict
	}
	return &users[0], nil
}

func BindUserEmail(userID int, email string) error {
	email = NormalizeEmail(email)
	if userID <= 0 || email == "" {
		return ErrEmailConflict
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockEmailOwnership(tx, email); err != nil {
			return err
		}
		var count int64
		if err := tx.Unscoped().Model(&identityschema.User{}).Where("LOWER(TRIM(email)) = ? AND id <> ?", email, userID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrEmailConflict
		}
		result := tx.Model(&identityschema.User{}).Where("id = ?", userID).Update("email", email)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		return err
	}
	return platformcache.DeleteUserCache(userID)
}

func ResetUserPasswordByEmail(email string, password string) error {
	hashedPassword, err := platformsecurity.Password2Hash(password)
	if err != nil {
		return err
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockEmailOwnership(tx, email); err != nil {
			return err
		}
		user, err := uniqueUserByEmail(tx, email)
		if err != nil {
			return err
		}
		return tx.Model(&identityschema.User{}).Where("id = ?", user.Id).Update("password", hashedPassword).Error
	})
}
