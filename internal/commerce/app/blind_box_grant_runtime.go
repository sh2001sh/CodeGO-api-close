package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const maxAdminBlindBoxGrantQuantity = 1000

type AdminBlindBoxGrantRequest struct {
	Quantity       int    `json:"quantity"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

type AdminBlindBoxGrantResult struct {
	Grant *commerceschema.BlindBoxGrant
	Order *commerceschema.BlindBoxOrder
}

func GrantBlindBoxes(userID int, adminUserID int, req AdminBlindBoxGrantRequest) (*AdminBlindBoxGrantResult, error) {
	if userID <= 0 || adminUserID <= 0 {
		return nil, errors.New("invalid blind box grant user")
	}
	if !blindboxsettings.Get().Enabled {
		return nil, commercedomain.ErrBlindBoxDisabled
	}
	if req.Quantity <= 0 || req.Quantity > maxAdminBlindBoxGrantQuantity {
		return nil, fmt.Errorf("blind box grant quantity must be between 1 and %d", maxAdminBlindBoxGrantQuantity)
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 255 {
		return nil, errors.New("blind box grant reason is too long")
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, errors.New("blind box grant idempotency key is required")
	}
	if len(idempotencyKey) > 128 {
		return nil, errors.New("blind box grant idempotency key is too long")
	}

	result := &AdminBlindBoxGrantResult{}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return createBlindBoxGrantTx(tx, userID, adminUserID, req.Quantity, reason, idempotencyKey, result)
	})
	if err == nil {
		return result, nil
	}

	var existing commerceschema.BlindBoxGrant
	if lookupErr := platformdb.DB.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error; lookupErr != nil {
		return nil, err
	}
	if existing.UserId != userID || existing.AdminUserId != adminUserID || existing.Quantity != req.Quantity || existing.Reason != reason {
		return nil, errors.New("blind box grant idempotency key already used")
	}
	var order commerceschema.BlindBoxOrder
	if lookupErr := platformdb.DB.Where("id = ?", existing.BlindBoxOrderId).First(&order).Error; lookupErr != nil {
		return nil, lookupErr
	}
	return &AdminBlindBoxGrantResult{Grant: &existing, Order: &order}, nil
}

func createBlindBoxGrantTx(tx *gorm.DB, userID int, adminUserID int, quantity int, reason string, idempotencyKey string, result *AdminBlindBoxGrantResult) error {
	var user identityschema.User
	if err := tx.Select("id").Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}
	var existing commerceschema.BlindBoxGrant
	if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error; err == nil {
		if existing.UserId != userID || existing.AdminUserId != adminUserID || existing.Quantity != quantity || existing.Reason != reason {
			return errors.New("blind box grant idempotency key already used")
		}
		var order commerceschema.BlindBoxOrder
		if err := tx.Where("id = ?", existing.BlindBoxOrderId).First(&order).Error; err != nil {
			return err
		}
		result.Grant = &existing
		result.Order = &order
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	now := platformruntime.GetTimestamp()
	grant := &commerceschema.BlindBoxGrant{
		UserId:         userID,
		AdminUserId:    adminUserID,
		Quantity:       quantity,
		Reason:         reason,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
	}
	if err := tx.Create(grant).Error; err != nil {
		return err
	}

	order := &commerceschema.BlindBoxOrder{
		UserId:          userID,
		Quantity:        quantity,
		Money:           0,
		TradeNo:         fmt.Sprintf("blind-box-grant-%d-%d-%s", now, grant.Id, platformruntime.GetRandomString(8)),
		PaymentMethod:   "admin_grant",
		PaymentProvider: "admin",
		Source:          commerceschema.BlindBoxOrderSourceAdminGrant,
		Status:          constant.TopUpStatusSuccess,
		CreateTime:      now,
		CompleteTime:    now,
	}
	if err := tx.Create(order).Error; err != nil {
		return err
	}
	grant.BlindBoxOrderId = order.Id
	grant.TradeNo = order.TradeNo
	if err := tx.Save(grant).Error; err != nil {
		return err
	}
	auditMessage := fmt.Sprintf("管理员发放盲盒，管理员ID：%d，数量：%d", adminUserID, quantity)
	if reason != "" {
		auditMessage += fmt.Sprintf("，原因：%s", reason)
	}
	auditMessage += fmt.Sprintf("，发放记录ID：%d", grant.Id)
	if err := auditapp.RecordLogTx(tx, userID, auditschema.LogTypeTopup, auditMessage); err != nil {
		return err
	}
	result.Grant = grant
	result.Order = order
	return nil
}

func ListUserBlindBoxGrants(userID int, limit int) ([]commerceschema.BlindBoxGrant, error) {
	if userID <= 0 {
		return []commerceschema.BlindBoxGrant{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	var grants []commerceschema.BlindBoxGrant
	err := platformdb.DB.Where("user_id = ?", userID).Order("created_at desc, id desc").Limit(limit).Find(&grants).Error
	return grants, err
}
