package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GiftBlindBoxPropsBatch transfers available blind-box props from the owner to recipients.
// The transfer is atomic per recipient and never creates props from thin air.
func GiftBlindBoxPropsBatch(senderUserID int, recipientExternalID string, quantity int, requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if senderUserID <= 0 || quantity <= 0 || quantity > 50 || requestID == "" || len(requestID) > 128 {
		return errors.New("盲盒道具赠送参数无效")
	}
	recipientExternalID = strings.ToUpper(strings.TrimSpace(recipientExternalID))
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var recipient identityschema.User
		if err := tx.Where("external_id = ? AND status = ?", recipientExternalID, constant.UserStatusEnabled).First(&recipient).Error; err != nil {
			return err
		}
		if recipient.Id == senderUserID {
			return errors.New("不能向自己赠送盲盒道具")
		}
		if _, err := lockTransferUsers(tx, senderUserID, recipient.Id); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("batch-gift:%d:%s", senderUserID, requestID)))
		prefix := fmt.Sprintf("bg:%x:", digest[:24])
		var existing []commerceschema.BlindBoxPropGift
		if err := tx.Where("request_id LIKE ?", prefix+"%").Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			if len(existing) != quantity {
				return errors.New("道具赠送请求冲突")
			}
			for _, gift := range existing {
				if gift.SenderUserId != senderUserID || gift.RecipientUserId != recipient.Id {
					return errors.New("道具赠送请求冲突")
				}
			}
			return nil
		}
		for i := 0; i < quantity; i++ {
			// A stable slot receipt makes retries independent of current inventory.
			key := fmt.Sprintf("%s%d", prefix, i)
			var gift commerceschema.BlindBoxPropGift
			err := tx.Where("request_id = ?", key).First(&gift).Error
			if err == nil {
				if gift.SenderUserId != senderUserID || gift.RecipientUserId != recipient.Id {
					return errors.New("道具赠送请求冲突")
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var prop commerceschema.BlindBoxProp
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND status = ?", senderUserID, commerceschema.BlindBoxPropStatusAvailable).Order("id asc").First(&prop).Error; err != nil {
				return errors.New("盲盒道具库存不足")
			}
			if err := executeBlindBoxPropGiftTx(tx, senderUserID, prop.Id, GiftBlindBoxPropRequest{RecipientExternalId: recipientExternalID, RequestId: key}, &gift, &recipient); err != nil {
				return err
			}
		}
		return nil
	})
}
