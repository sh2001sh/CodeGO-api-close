package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const balanceBlindBoxGiftCoolingSeconds = int64(72 * 60 * 60)

var ErrBalanceBlindBoxGiftCooling = errors.New("新用户购买的盲盒仍在冷却期，暂不可赠送")

type GiftBalanceBlindBoxRequest struct {
	RecipientExternalId string `json:"recipient_external_id"`
	RequestId           string `json:"request_id"`
	Count               int    `json:"count"`
}

// GiftBalanceBlindBoxes transfers sealed inventory while preserving its issued reward and guarantees.
func GiftBalanceBlindBoxes(senderUserID int, req GiftBalanceBlindBoxRequest) (*BalanceBlindBoxGiftResult, error) {
	req.RecipientExternalId = strings.ToUpper(strings.TrimSpace(req.RecipientExternalId))
	req.RequestId = strings.TrimSpace(req.RequestId)
	if senderUserID <= 0 || len(req.RecipientExternalId) != identityschema.ExternalUserIDLength || req.RequestId == "" || len(req.RequestId) > 64 || req.Count <= 0 || req.Count > balanceBlindBoxMaxBatch {
		return nil, errors.New("余额盲盒赠送参数无效")
	}
	var gift commerceschema.BalanceBlindBoxGift
	var recipient identityschema.User
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if existing, found, err := loadBalanceBlindBoxGiftReplay(tx, senderUserID, req); err != nil {
			return err
		} else if found {
			gift = *existing
			return tx.Select("id, external_id, display_name, username, status").First(&recipient, gift.RecipientUserId).Error
		}
		return executeBalanceBlindBoxGiftTx(tx, senderUserID, req, &gift, &recipient)
	})
	if err != nil {
		return nil, err
	}
	overview, err := GetBalanceBlindBoxOverview(senderUserID)
	if err != nil {
		return nil, err
	}
	return &BalanceBlindBoxGiftResult{Gift: gift, Overview: *overview, Recipient: *recipientFromUser(&recipient)}, nil
}

func executeBalanceBlindBoxGiftTx(tx *gorm.DB, senderUserID int, req GiftBalanceBlindBoxRequest, gift *commerceschema.BalanceBlindBoxGift, recipient *identityschema.User) error {
	if err := tx.Where("external_id = ? AND status = ?", req.RecipientExternalId, constant.UserStatusEnabled).First(recipient).Error; err != nil {
		return commerceschema.ErrWalletTransferRecipientNotFound
	}
	if recipient.Id == senderUserID {
		return commerceschema.ErrWalletTransferSelf
	}
	users, err := lockTransferUsers(tx, senderUserID, recipient.Id)
	if err != nil {
		return err
	}
	sender, recipientRow := users[senderUserID], users[recipient.Id]
	items, err := lockBalanceBlindBoxGiftItems(tx, senderUserID, req.Count)
	if err != nil {
		return err
	}
	if err := ensureBalanceBlindBoxGiftCoolingPassed(tx, items); err != nil {
		return err
	}
	*gift = commerceschema.BalanceBlindBoxGift{
		RequestId: req.RequestId, SenderUserId: senderUserID, RecipientUserId: recipient.Id,
		SenderExternalId: sender.ExternalId, RecipientExternalId: recipientRow.ExternalId,
		SenderDisplayNameMasked:    maskWalletTransferName(displayNameForTransfer(sender)),
		RecipientDisplayNameMasked: maskWalletTransferName(displayNameForTransfer(recipientRow)), Quantity: req.Count,
	}
	if err := tx.Create(gift).Error; err != nil {
		return err
	}
	if err := transferBalanceBlindBoxItemsTx(tx, gift.Id, senderUserID, recipient.Id, items); err != nil {
		return err
	}
	if err := auditapp.RecordLogTx(tx, senderUserID, auditschema.LogTypeManage, fmt.Sprintf("向用户 %s 赠送余额盲盒 %d 个，赠送记录 %d", recipient.ExternalId, req.Count, gift.Id)); err != nil {
		return err
	}
	return auditapp.RecordLogTx(tx, recipient.Id, auditschema.LogTypeManage, fmt.Sprintf("收到用户 %s 赠送的余额盲盒 %d 个，赠送记录 %d", sender.ExternalId, req.Count, gift.Id))
}

func ensureBalanceBlindBoxGiftCoolingPassed(tx *gorm.DB, items []commerceschema.BalanceBlindBoxItem) error {
	if len(items) == 0 {
		return nil
	}
	sourceIDs := make([]int, 0, len(items))
	seen := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item.PurchaseUserId <= 0 {
			continue
		}
		if _, found := seen[item.PurchaseUserId]; found {
			continue
		}
		seen[item.PurchaseUserId] = struct{}{}
		sourceIDs = append(sourceIDs, item.PurchaseUserId)
	}
	if len(sourceIDs) == 0 {
		return nil
	}
	cutoff := platformruntime.GetTimestamp() - balanceBlindBoxGiftCoolingSeconds
	var coolingUsers int64
	if err := tx.Model(&identityschema.User{}).
		Where("id IN ? AND created_at > ?", sourceIDs, cutoff).
		Count(&coolingUsers).Error; err != nil {
		return err
	}
	if coolingUsers > 0 {
		return ErrBalanceBlindBoxGiftCooling
	}
	return nil
}

func lockBalanceBlindBoxGiftItems(tx *gorm.DB, senderUserID, count int) ([]commerceschema.BalanceBlindBoxItem, error) {
	var items []commerceschema.BalanceBlindBoxItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_user_id = ? AND status = ?", senderUserID, commerceschema.BalanceBlindBoxItemStatusAvailable).Order("id asc").Limit(count).Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) != count {
		return nil, fmt.Errorf("余额盲盒库存不足，当前可赠送 %d 个", len(items))
	}
	return items, nil
}

func transferBalanceBlindBoxItemsTx(tx *gorm.DB, giftID, senderUserID, recipientUserID int, items []commerceschema.BalanceBlindBoxItem) error {
	for index := range items {
		link := commerceschema.BalanceBlindBoxGiftItem{GiftId: giftID, ItemId: items[index].Id, FromUserId: senderUserID, ToUserId: recipientUserID}
		if err := tx.Create(&link).Error; err != nil {
			return err
		}
		result := tx.Model(&items[index]).Where("id = ? AND owner_user_id = ? AND status = ?", items[index].Id, senderUserID, commerceschema.BalanceBlindBoxItemStatusAvailable).Update("owner_user_id", recipientUserID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func loadBalanceBlindBoxGiftReplay(tx *gorm.DB, senderUserID int, req GiftBalanceBlindBoxRequest) (*commerceschema.BalanceBlindBoxGift, bool, error) {
	var gift commerceschema.BalanceBlindBoxGift
	err := tx.Where("request_id = ?", req.RequestId).First(&gift).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if gift.SenderUserId != senderUserID || gift.RecipientExternalId != req.RecipientExternalId || gift.Quantity != req.Count {
		return nil, true, errors.New("余额盲盒赠送请求冲突")
	}
	return &gift, true, nil
}
