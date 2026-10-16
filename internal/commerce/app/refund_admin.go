package app

import (
	"errors"
	"fmt"

	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
)

var ErrRefundAdminRequired = errors.New("退款仅限管理员操作")

// Check the persisted role even when an internal caller bypasses HTTP middleware.
func authorizeRefundAdmin(adminID, userID int) error {
	if adminID <= 0 {
		return ErrRefundAdminRequired
	}
	var admin identityschema.User
	if err := platformdb.DB.Select("id", "role", "status").First(&admin, adminID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			platformobservability.SysError(fmt.Sprintf("verify refund administrator %d: %v", adminID, err))
		}
		return ErrRefundAdminRequired
	}
	if admin.Role < constant.RoleAdminUser || admin.Status != constant.UserStatusEnabled {
		return ErrRefundAdminRequired
	}
	if userID <= 0 {
		return errors.New("退款用户参数无效")
	}
	var user identityschema.User
	if err := platformdb.DB.Select("id").First(&user, userID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			platformobservability.SysError(fmt.Sprintf("verify refund recipient %d: %v", userID, err))
			return errors.New("核验退款用户失败")
		}
		return errors.New("退款用户不存在")
	}
	return nil
}

func ListAdminRefundableOrders(adminID, userID int) ([]commerceschema.RefundableOrder, error) {
	if err := authorizeRefundAdmin(adminID, userID); err != nil {
		return nil, err
	}
	return listRefundableOrders(userID)
}

func CreateAdminRefund(adminID, userID int, req commerceschema.RefundRequest) (*commerceschema.RefundResult, error) {
	if err := authorizeRefundAdmin(adminID, userID); err != nil {
		return nil, err
	}
	recordAdminRefundOperation(adminID, userID, "request", req.OrderType, req.TradeNo, nil)
	result, err := createRefund(userID, req)
	if err != nil {
		recordAdminRefundOperation(adminID, userID, "request_failed_or_pending", req.OrderType, req.TradeNo, result)
		return result, err
	}
	recordAdminRefundOperation(adminID, userID, "request_result", req.OrderType, req.TradeNo, result)
	return result, nil
}

func SyncAdminRefund(adminID, userID int, refundNo string) (*commerceschema.RefundResult, error) {
	if err := authorizeRefundAdmin(adminID, userID); err != nil {
		return nil, err
	}
	recordAdminRefundOperation(adminID, userID, "sync_request", "", "", &commerceschema.RefundResult{RefundNo: refundNo})
	result, err := syncRefund(userID, refundNo)
	if err != nil {
		return nil, err
	}
	recordAdminRefundOperation(adminID, userID, "sync", result.OrderType, result.TradeNo, result)
	return result, nil
}

func recordAdminRefundOperation(adminID, userID int, action, orderType, tradeNo string, result *commerceschema.RefundResult) {
	info := map[string]interface{}{"admin_id": adminID, "action": action, "order_type": orderType, "trade_no": tradeNo}
	if result != nil {
		info["refund_no"], info["refund_amount"], info["refund_status"] = result.RefundNo, result.RefundAmount, result.Status
	}
	auditapp.RecordLogWithAdminInfo(userID, auditschema.LogTypeManage, fmt.Sprintf("admin refund %s", action), info)
}
