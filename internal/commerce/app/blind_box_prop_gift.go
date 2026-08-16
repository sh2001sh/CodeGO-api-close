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
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GiftBlindBoxPropRequest struct {
	RecipientExternalId string `json:"recipient_external_id"`
	RequestId           string `json:"request_id"`
}

type BlindBoxPropGiftResult struct {
	Gift      commerceschema.BlindBoxPropGift `json:"gift"`
	Recipient WalletTransferRecipient         `json:"recipient"`
}

// GiftBlindBoxProp transfers one unused prop to another enabled user.
func GiftBlindBoxProp(senderUserID, propID int, req GiftBlindBoxPropRequest) (*BlindBoxPropGiftResult, error) {
	req.RecipientExternalId = strings.ToUpper(strings.TrimSpace(req.RecipientExternalId))
	req.RequestId = strings.TrimSpace(req.RequestId)
	if !validBlindBoxPropGiftRequest(senderUserID, propID, req) {
		return nil, errors.New("道具赠送参数无效")
	}

	var gift commerceschema.BlindBoxPropGift
	var recipient identityschema.User
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if replay, found, err := loadBlindBoxPropGiftReplay(tx, senderUserID, propID, req); err != nil {
			return err
		} else if found {
			gift = *replay
			return tx.First(&recipient, gift.RecipientUserId).Error
		}
		return executeBlindBoxPropGiftTx(tx, senderUserID, propID, req, &gift, &recipient)
	})
	if err != nil {
		return nil, err
	}
	return &BlindBoxPropGiftResult{Gift: gift, Recipient: *recipientFromUser(&recipient)}, nil
}

func validBlindBoxPropGiftRequest(senderUserID, propID int, req GiftBlindBoxPropRequest) bool {
	return senderUserID > 0 && propID > 0 &&
		len(req.RecipientExternalId) == identityschema.ExternalUserIDLength &&
		req.RequestId != "" && len(req.RequestId) <= 64
}

func executeBlindBoxPropGiftTx(tx *gorm.DB, senderUserID, propID int, req GiftBlindBoxPropRequest, gift *commerceschema.BlindBoxPropGift, recipient *identityschema.User) error {
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
	prop, err := lockGiftableBlindBoxPropTx(tx, senderUserID, propID)
	if err != nil {
		return err
	}

	*gift = commerceschema.BlindBoxPropGift{
		RequestId: req.RequestId, PropId: prop.Id,
		SenderUserId: senderUserID, RecipientUserId: recipient.Id,
		SenderExternalId: users[senderUserID].ExternalId, RecipientExternalId: users[recipient.Id].ExternalId,
		PropType: prop.PropType, PropTitle: prop.Title,
	}
	if err := tx.Create(gift).Error; err != nil {
		return err
	}
	result := tx.Model(prop).
		Where("id = ? AND user_id = ? AND status = ?", prop.Id, senderUserID, commerceschema.BlindBoxPropStatusAvailable).
		Update("user_id", recipient.Id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("道具状态已变化，请刷新后重试")
	}
	return recordBlindBoxPropGiftAuditTx(tx, gift, users[senderUserID], users[recipient.Id])
}

func lockGiftableBlindBoxPropTx(tx *gorm.DB, senderUserID, propID int) (*commerceschema.BlindBoxProp, error) {
	var prop commerceschema.BlindBoxProp
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", propID, senderUserID).First(&prop).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("道具不存在或不属于当前用户")
	}
	if err != nil {
		return nil, err
	}
	if prop.Status != commerceschema.BlindBoxPropStatusAvailable {
		return nil, errors.New("只有未使用且未锁定的道具可以赠送")
	}
	return &prop, nil
}

func recordBlindBoxPropGiftAuditTx(tx *gorm.DB, gift *commerceschema.BlindBoxPropGift, sender, recipient *identityschema.User) error {
	if err := auditapp.RecordLogTx(tx, sender.Id, auditschema.LogTypeManage, fmt.Sprintf("向用户 %s 赠送道具 %s（道具ID：%d，赠送记录：%d）", recipient.ExternalId, gift.PropTitle, gift.PropId, gift.Id)); err != nil {
		return err
	}
	return auditapp.RecordLogTx(tx, recipient.Id, auditschema.LogTypeManage, fmt.Sprintf("收到用户 %s 赠送的道具 %s（道具ID：%d，赠送记录：%d）", sender.ExternalId, gift.PropTitle, gift.PropId, gift.Id))
}

func loadBlindBoxPropGiftReplay(tx *gorm.DB, senderUserID, propID int, req GiftBlindBoxPropRequest) (*commerceschema.BlindBoxPropGift, bool, error) {
	var gift commerceschema.BlindBoxPropGift
	err := tx.Where("request_id = ?", req.RequestId).First(&gift).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if gift.SenderUserId != senderUserID || gift.PropId != propID || gift.RecipientExternalId != req.RecipientExternalId {
		return nil, true, errors.New("道具赠送请求冲突")
	}
	return &gift, true, nil
}
