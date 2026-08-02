package app

import (
	"errors"
	"fmt"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const walletQuotaConversionHistoryLimit = 10

type CreateWalletQuotaConversionRequest struct {
	Direction   string `json:"direction"`
	SourceQuota int64  `json:"source_quota"`
	RequestId   string `json:"request_id"`
}

type WalletQuotaConversionOverview struct {
	StandardPerClaude int64                                  `json:"standard_per_claude"`
	QuotaPerUSD       int64                                  `json:"quota_per_usd"`
	StandardQuota     int64                                  `json:"standard_quota"`
	ClaudeQuota       int64                                  `json:"claude_quota"`
	RecentConversions []commerceschema.WalletQuotaConversion `json:"recent_conversions"`
}

// BuildWalletQuotaConversionOverview returns current balances, rate, and recent transfers.
func BuildWalletQuotaConversionOverview(userID int) (*WalletQuotaConversionOverview, error) {
	if userID <= 0 {
		return nil, commerceschema.ErrWalletQuotaConversionInvalid
	}
	standardQuota, err := billingapp.GetUserWalletQuota(userID)
	if err != nil {
		return nil, err
	}
	claudeQuota, err := billingapp.GetUserClaudeWalletQuota(userID)
	if err != nil {
		return nil, err
	}
	items, err := ListRecentWalletQuotaConversions(userID, walletQuotaConversionHistoryLimit)
	if err != nil {
		return nil, err
	}
	return &WalletQuotaConversionOverview{
		StandardPerClaude: commerceschema.WalletQuotaStandardPerClaude,
		QuotaPerUSD:       int64(platformruntime.QuotaPerUnit),
		StandardQuota:     int64(standardQuota),
		ClaudeQuota:       int64(claudeQuota),
		RecentConversions: items,
	}, nil
}

// ListRecentWalletQuotaConversions returns the user's latest completed conversions.
func ListRecentWalletQuotaConversions(userID int, limit int) ([]commerceschema.WalletQuotaConversion, error) {
	if userID <= 0 {
		return []commerceschema.WalletQuotaConversion{}, nil
	}
	if limit <= 0 {
		limit = walletQuotaConversionHistoryLimit
	}
	if limit > 50 {
		limit = 50
	}
	var items []commerceschema.WalletQuotaConversion
	err := platformdb.DB.Where("user_id = ?", userID).Order("id desc").Limit(limit).Find(&items).Error
	return items, err
}

// ConvertWalletQuota transfers quota between the standard and Claude wallets.
func ConvertWalletQuota(userID int, req CreateWalletQuotaConversionRequest) (*commerceschema.WalletQuotaConversion, error) {
	requestID := strings.TrimSpace(req.RequestId)
	if userID <= 0 || requestID == "" || len(requestID) > 128 || req.SourceQuota <= 0 {
		return nil, commerceschema.ErrWalletQuotaConversionInvalid
	}
	if req.Direction != commerceschema.WalletQuotaConversionStandardToClaude &&
		req.Direction != commerceschema.WalletQuotaConversionClaudeToStandard {
		return nil, commerceschema.ErrWalletQuotaConversionInvalid
	}

	var conversion commerceschema.WalletQuotaConversion
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var user identityschema.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		existing := commerceschema.WalletQuotaConversion{}
		if err := tx.Where("request_id = ?", requestID).First(&existing).Error; err == nil {
			if existing.UserId != userID || existing.Direction != req.Direction || existing.SourceQuota != req.SourceQuota {
				return commerceschema.ErrWalletQuotaConversionInvalid
			}
			conversion = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		targetQuota, err := calculateWalletQuotaConversionTarget(req.Direction, req.SourceQuota)
		if err != nil {
			return err
		}
		standardBefore := int64(user.Quota)
		claudeBefore := int64(user.ClaudeQuota)
		operationID := "wallet-conversion:" + requestID

		switch req.Direction {
		case commerceschema.WalletQuotaConversionStandardToClaude:
			if standardBefore < req.SourceQuota {
				return commerceschema.ErrWalletQuotaConversionInsufficient
			}
			if err := billingapp.DebitWalletQuotaTx(tx, userID, int(req.SourceQuota), operationID+":debit"); err != nil {
				return mapWalletQuotaDebitError(err)
			}
			if err := billingapp.CreditClaudeWalletQuotaTx(tx, userID, int(targetQuota), operationID+":credit", "wallet_quota_conversion_credit"); err != nil {
				return err
			}
		case commerceschema.WalletQuotaConversionClaudeToStandard:
			if claudeBefore < req.SourceQuota {
				return commerceschema.ErrWalletQuotaConversionInsufficient
			}
			if err := billingapp.DebitClaudeWalletQuotaTx(tx, userID, int(req.SourceQuota), operationID+":debit"); err != nil {
				return mapWalletQuotaDebitError(err)
			}
			if err := billingapp.CreditWalletQuotaTx(tx, userID, int(targetQuota), operationID+":credit", "wallet_quota_conversion_credit"); err != nil {
				return err
			}
		}

		var balances struct {
			Quota       int64 `gorm:"column:quota"`
			ClaudeQuota int64 `gorm:"column:claude_quota"`
		}
		if err := tx.Model(&identityschema.User{}).Select("quota, claude_quota").Where("id = ?", userID).Scan(&balances).Error; err != nil {
			return err
		}
		conversion = commerceschema.WalletQuotaConversion{
			UserId:              userID,
			RequestId:           requestID,
			Direction:           req.Direction,
			Status:              commerceschema.WalletQuotaConversionStatusCompleted,
			SourceQuota:         req.SourceQuota,
			TargetQuota:         targetQuota,
			StandardQuotaBefore: standardBefore,
			StandardQuotaAfter:  balances.Quota,
			ClaudeQuotaBefore:   claudeBefore,
			ClaudeQuotaAfter:    balances.ClaudeQuota,
		}
		return tx.Create(&conversion).Error
	})
	if err != nil {
		return nil, err
	}
	_ = identitystore.InvalidateUserCache(userID)
	return &conversion, nil
}

func calculateWalletQuotaConversionTarget(direction string, sourceQuota int64) (int64, error) {
	if sourceQuota <= 0 {
		return 0, commerceschema.ErrWalletQuotaConversionInvalid
	}
	if direction == commerceschema.WalletQuotaConversionStandardToClaude {
		if sourceQuota%commerceschema.WalletQuotaStandardPerClaude != 0 {
			return 0, commerceschema.ErrWalletQuotaConversionInexact
		}
		return sourceQuota / commerceschema.WalletQuotaStandardPerClaude, nil
	}
	maxInt := int64(^uint(0) >> 1)
	if sourceQuota > maxInt/commerceschema.WalletQuotaStandardPerClaude {
		return 0, fmt.Errorf("%w: target overflow", commerceschema.ErrWalletQuotaConversionInvalid)
	}
	return sourceQuota * commerceschema.WalletQuotaStandardPerClaude, nil
}

func mapWalletQuotaDebitError(err error) error {
	if errors.Is(err, billingdomain.ErrInsufficientBalance) {
		return commerceschema.ErrWalletQuotaConversionInsufficient
	}
	return err
}
