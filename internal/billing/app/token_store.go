package app

import (
	"errors"
	"fmt"
	"strings"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TokenSnapshot struct {
	ID             int
	Key            string
	ExpiredTime    int64
	RemainQuota    int
	UsedQuota      int
	UnlimitedQuota bool
}

func GetTokenByKey(tokenKey string) (*TokenSnapshot, error) {
	token, err := identitystore.LoadTokenByKey(strings.TrimSpace(tokenKey), false)
	if err != nil {
		return nil, err
	}
	snapshot := tokenSnapshotFromModel(token)
	if snapshot.UnlimitedQuota {
		return snapshot, nil
	}
	remainQuota, err := getLedgerBackedTokenQuota(token)
	if err != nil {
		return nil, err
	}
	snapshot.RemainQuota = remainQuota
	return snapshot, nil
}

func GetTokenByID(tokenID int) (*TokenSnapshot, error) {
	if tokenID <= 0 {
		return nil, errors.New("id 为空！")
	}
	token := &identityschema.Token{Id: tokenID}
	err := platformdb.DB.First(token, "id = ?", tokenID).Error
	if err != nil {
		return nil, err
	}
	snapshot := tokenSnapshotFromModel(token)
	if snapshot.UnlimitedQuota {
		return snapshot, nil
	}
	remainQuota, err := getLedgerBackedTokenQuota(token)
	if err != nil {
		return nil, err
	}
	snapshot.RemainQuota = remainQuota
	return snapshot, nil
}

func GetUserUsedQuota(userID int) (int, error) {
	var quota int
	err := platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Select("used_quota").Find(&quota).Error
	return quota, err
}

func AdjustTokenQuota(tokenID int, tokenKey string, delta int) error {
	if tokenID <= 0 || delta == 0 {
		return nil
	}
	projectCache := false
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		token, err := lockTokenForQuotaAdjustment(tx, tokenID)
		if err != nil {
			return err
		}
		if token.UnlimitedQuota {
			return nil
		}

		if int64(token.RemainQuota)-int64(delta) < 0 {
			return identitystore.ErrTokenQuotaInsufficient
		}
		account, err := ensureMirroredTokenAccountTx(tx, token)
		if err != nil {
			return err
		}
		snapshot, err := loadBalanceSnapshotTx(tx, account.AccountID)
		if err != nil {
			return err
		}
		operationID := platformruntime.GetUUID()
		driftUsage := snapshot.AvailableBalance - int64(token.RemainQuota)
		if _, err := billingdomain.AdjustAvailableBalanceTx(tx, billingdomain.AdjustAvailableBalanceParams{
			AccountID:      account.AccountID,
			UsageAmount:    driftUsage,
			IdempotencyKey: "token-reconcile:" + operationID,
			ReasonCode:     "token_quota_reconcile",
			ReferenceType:  "token",
			ReferenceID:    fmt.Sprintf("%d", token.Id),
		}); err != nil {
			return err
		}
		if _, err := billingdomain.AdjustAvailableBalanceTx(tx, billingdomain.AdjustAvailableBalanceParams{
			AccountID:      account.AccountID,
			UsageAmount:    int64(delta),
			IdempotencyKey: "token-adjust:" + operationID,
			ReasonCode:     "token_quota_adjustment",
			ReferenceType:  "token",
			ReferenceID:    fmt.Sprintf("%d", token.Id),
		}); err != nil {
			return err
		}
		if err := identitystore.AdjustTokenQuotaTx(tx, token.Id, delta); err != nil {
			return err
		}
		projectCache = true
		return nil
	})
	if err != nil {
		return err
	}
	if projectCache {
		identitystore.ProjectTokenQuotaCache(strings.TrimSpace(tokenKey), -int64(delta))
	}
	return nil
}

func getLedgerBackedTokenQuota(token *identityschema.Token) (int, error) {
	if token == nil || token.Id <= 0 {
		return 0, errors.New("invalid token")
	}
	var account billingschema.BillingAccount
	err := platformdb.DB.Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?", "token", token.Id, "token", "quota").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || isMissingBillingSchema(err) {
		return token.RemainQuota, nil
	}
	if err != nil {
		return 0, err
	}
	snapshot, err := loadBalanceSnapshot(account.AccountID)
	if err != nil {
		return 0, err
	}
	return int(snapshot.AvailableBalance), nil
}

func lockTokenForQuotaAdjustment(tx *gorm.DB, tokenID int) (*identityschema.Token, error) {
	var token identityschema.Token
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", tokenID).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

func ensureMirroredTokenAccountTx(tx *gorm.DB, token *identityschema.Token) (*billingschema.BillingAccount, error) {
	if token == nil || token.Id <= 0 {
		return nil, errors.New("invalid token")
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: "token", OwnerType: "token", OwnerID: int64(token.Id), QuotaUnit: "quota",
	})
	if err != nil {
		return nil, err
	}
	snapshot, err := loadBalanceSnapshotTx(tx, account.AccountID)
	if err != nil {
		return nil, err
	}
	if hasNonZeroSnapshot(snapshot) || token.RemainQuota <= 0 {
		return account, nil
	}
	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: int64(token.RemainQuota),
		IdempotencyKey: fmt.Sprintf("mirror-bootstrap:token:%d", token.Id), ReasonCode: "mirror_bootstrap",
		ReferenceType: "token", ReferenceID: fmt.Sprintf("%d", token.Id), OperatorType: "ledger_sync", OperatorID: "mirror_bootstrap",
	})
	if err != nil {
		return nil, err
	}
	return account, nil
}

func tokenSnapshotFromModel(token *identityschema.Token) *TokenSnapshot {
	if token == nil {
		return nil
	}
	return &TokenSnapshot{
		ID:             token.Id,
		Key:            token.Key,
		ExpiredTime:    token.ExpiredTime,
		RemainQuota:    token.RemainQuota,
		UsedQuota:      token.UsedQuota,
		UnlimitedQuota: token.UnlimitedQuota,
	}
}
