package app

import (
	"errors"
	"fmt"
	"strings"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OpenBalanceBlindBox opens sealed inventory without charging the wallet again.
func OpenBalanceBlindBox(userID int, requestID string, requestedCount ...int) (*BalanceBlindBoxOpenResult, error) {
	requestID = strings.TrimSpace(requestID)
	count := 1
	if len(requestedCount) > 0 {
		count = requestedCount[0]
	}
	if userID <= 0 || requestID == "" || len(requestID) > 61 || count <= 0 || count > balanceBlindBoxMaxBatch {
		return nil, errors.New("统一盲盒开启参数无效，单次最多开启 100 个")
	}
	setting := blindboxsettings.Get()
	if !setting.Enabled || !setting.BalanceBlindBoxEnabled {
		return nil, commercedomain.ErrBlindBoxDisabled
	}

	result := &BalanceBlindBoxOpenResult{Records: make([]commerceschema.BlindBoxOpenRecord, 0, count)}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if replayed, err := loadBalanceBlindBoxOpenReplay(tx, userID, requestID, count); err != nil {
			return err
		} else if len(replayed) > 0 {
			result.Records, result.Record = replayed, replayed[0]
			return nil
		}

		var items []commerceschema.BalanceBlindBoxItem
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_user_id = ? AND status = ?", userID, commerceschema.BalanceBlindBoxItemStatusAvailable).
			Order("id asc").Limit(count).Find(&items).Error
		if err != nil {
			return err
		}
		if len(items) != count {
			return fmt.Errorf("统一盲盒库存不足，当前可开启 %d 个", len(items))
		}
		for index := range items {
			drawRequestID := requestID
			if index > 0 {
				drawRequestID = fmt.Sprintf("%s:%d", requestID, index)
			}
			record, err := openSealedBalanceBlindBoxItemTx(tx, userID, drawRequestID, &items[index])
			if err != nil {
				return err
			}
			result.Records = append(result.Records, *record)
		}
		result.Record = result.Records[0]
		return auditapp.RecordLogTx(tx, userID, auditschema.LogTypeManage, fmt.Sprintf("开启统一盲盒库存 %d 个", count))
	})
	if err != nil {
		return nil, err
	}
	_ = identitystore.InvalidateUserCache(userID)
	overview, err := GetBalanceBlindBoxOverview(userID)
	if err != nil {
		return nil, err
	}
	result.BalanceUSD, result.Overview = overview.BalanceUSD, *overview
	return result, nil
}

func loadBalanceBlindBoxOpenReplay(tx *gorm.DB, userID int, requestID string, count int) ([]commerceschema.BlindBoxOpenRecord, error) {
	requestIDs := make([]string, count)
	for index := 0; index < count; index++ {
		requestIDs[index] = requestID
		if index > 0 {
			requestIDs[index] = fmt.Sprintf("%s:%d", requestID, index)
		}
	}
	var records []commerceschema.BlindBoxOpenRecord
	err := tx.Where("user_id = ? AND pool_type IN ? AND request_id IN ?", userID, []string{commerceschema.BlindBoxPoolTypeUnified, commerceschema.BlindBoxPoolTypeBalance15}, requestIDs).
		Order("id asc").Find(&records).Error
	if err != nil || len(records) == 0 {
		return records, err
	}
	if len(records) != count {
		return nil, errors.New("统一盲盒开启请求冲突")
	}
	return records, nil
}

func openSealedBalanceBlindBoxItemTx(tx *gorm.DB, userID int, requestID string, item *commerceschema.BalanceBlindBoxItem) (*commerceschema.BlindBoxOpenRecord, error) {
	walletType := normalizeBlindBoxRewardWalletType(item.RewardWalletType)
	record := &commerceschema.BlindBoxOpenRecord{
		UserId: userID, RequestId: &requestID, PoolType: commerceschema.BlindBoxPoolTypeUnified,
		RewardType: item.RewardType, RewardTier: item.RewardTier, RewardUSD: item.RewardUSD,
		CreditAmount: item.CreditAmount, RewardTitle: item.RewardTitle,
		RewardWalletType: item.RewardWalletType, IsPity: item.IsPity, CreateTime: platformruntime.GetTimestamp(),
	}
	if err := createBlindBoxOpenRecordTx(tx, record); err != nil {
		return nil, err
	}
	if item.RewardType == commerceschema.BlindBoxRewardTypeProp {
		prop, err := createBlindBoxPropTx(tx, userID, record.Id, item.RewardTitle)
		if err != nil {
			return nil, err
		}
		record.PropId, record.PropType, record.PropStatus, record.PropExpiresAt = prop.Id, prop.PropType, prop.Status, prop.ExpiresAt
	} else {
		if item.CreditAmount <= 0 {
			return nil, fmt.Errorf("余额盲盒 %d 的封存奖励无效", item.Id)
		}
		if err := applyBlindBoxWalletRewardTx(tx, userID, record.Id, item.CreditAmount, walletType); err != nil {
			return nil, err
		}
		if err := recordBlindBoxRewardLogTx(tx, userID, item.CreditAmount, walletType, record); err != nil {
			return nil, err
		}
	}
	now := platformruntime.GetTimestamp()
	result := tx.Model(item).Where("id = ? AND owner_user_id = ? AND status = ?", item.Id, userID, commerceschema.BalanceBlindBoxItemStatusAvailable).
		Updates(map[string]any{"status": commerceschema.BalanceBlindBoxItemStatusOpened, "open_record_id": record.Id, "opened_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return record, nil
}
