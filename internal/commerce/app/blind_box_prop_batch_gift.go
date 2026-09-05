package app

import (
	"errors"
	"fmt"
	"strings"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GiftBlindBoxPropsBatch transfers available blind-box props from the owner to recipients.
// The transfer is atomic per recipient and never creates props from thin air.
func GiftBlindBoxPropsBatch(senderUserID int, recipientExternalID string, quantity int, requestID string) error {
	if senderUserID <= 0 || quantity <= 0 || strings.TrimSpace(requestID) == "" {
		return errors.New("盲盒道具赠送参数无效")
	}
	recipientExternalID = strings.ToUpper(strings.TrimSpace(recipientExternalID))
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var recipient identityschema.User
		if err := tx.Where("external_id = ?", recipientExternalID).First(&recipient).Error; err != nil {
			return err
		}
		if recipient.Id == senderUserID {
			return errors.New("不能向自己赠送盲盒道具")
		}
		var props []commerceschema.BlindBoxProp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND status = ?", senderUserID, commerceschema.BlindBoxPropStatusAvailable).Order("id asc").Limit(quantity).Find(&props).Error; err != nil {
			return err
		}
		if len(props) < quantity {
			return fmt.Errorf("盲盒道具库存不足，当前可赠送 %d 个", len(props))
		}
		for _, prop := range props {
			if err := tx.Model(&commerceschema.BlindBoxProp{}).Where("id = ? AND user_id = ? AND status = ?", prop.Id, senderUserID, commerceschema.BlindBoxPropStatusAvailable).Update("user_id", recipient.Id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
