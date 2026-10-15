package app

import (
	"errors"
	"fmt"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RevokeBlindBoxes removes unopened boxes from a user inventory. Opened boxes are immutable.
func RevokeBlindBoxes(userID, adminID, quantity int, reason string) (int, error) {
	if userID <= 0 || adminID <= 0 || quantity <= 0 {
		return 0, errors.New("invalid revoke request")
	}
	revoked := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var items []commerceschema.BalanceBlindBoxItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_user_id=? AND status=?", userID, commerceschema.BalanceBlindBoxItemStatusAvailable).Order("id asc").Limit(quantity).Find(&items).Error; err != nil {
			return err
		}
		if len(items) < quantity {
			return errors.New("insufficient available blind boxes")
		}
		ids := make([]int, len(items))
		for i := range items {
			ids[i] = items[i].Id
		}
		res := tx.Model(&commerceschema.BalanceBlindBoxItem{}).Where("id IN ? AND owner_user_id=? AND status=?", ids, userID, commerceschema.BalanceBlindBoxItemStatusAvailable).Updates(map[string]any{"status": commerceschema.BalanceBlindBoxItemStatusRevoked})
		if res.Error != nil {
			return res.Error
		}
		revoked = int(res.RowsAffected)
		return auditapp.RecordLogTx(tx, userID, auditschema.LogTypeManage, "管理员扣除盲盒，管理员ID："+fmt.Sprint(adminID)+"，数量："+fmt.Sprint(revoked)+"，原因："+reason)
	})
	return revoked, err
}
