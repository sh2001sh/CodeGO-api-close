package app

import (
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"gorm.io/gorm"
)

const referralInviteeRegisterRewardPoints int64 = 2
const registrationBlindBoxQuantity = 5

func insertUserAndApplyRegistrationRewards(user *identityschema.User, inviterID int) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	if err := identitystore.CreateUser(user, inviterID); err != nil {
		return err
	}
	if inviterID > 0 {
		if err := grantRegistrationBlindBoxes(user.Id, inviterID); err != nil {
			return err
		}
	}
	recordRegistrationBonusLog(user.Id)
	applyReferralRegistrationRewards(inviterID, user.Id)
	return nil
}

func finalizeOAuthUserAndApplyRegistrationRewards(user *identityschema.User, inviterID int) {
	if user == nil {
		return
	}
	if err := identitystore.FinalizeCreatedUser(user.Id); err != nil {
		platformobservability.SysLog(fmt.Sprintf("failed to finalize created user %d: %v", user.Id, err))
	}
	if inviterID > 0 {
		if err := grantRegistrationBlindBoxes(user.Id, inviterID); err != nil {
			platformobservability.SysLog(fmt.Sprintf("failed to grant registration blind boxes to user %d: %v", user.Id, err))
		}
	}
	recordRegistrationBonusLog(user.Id)
	applyReferralRegistrationRewards(inviterID, user.Id)
}

func grantRegistrationBlindBoxes(userID int, inviterID int) error {
	if userID <= 0 || inviterID <= 0 {
		return nil
	}

	now := platformruntime.GetTimestamp()
	setting := blindboxsettings.Get()
	if !setting.RegistrationRewardActive(now) {
		return nil
	}
	var inviter identityschema.User
	err := platformdb.DB.Select("id").
		Where("id = ? AND role = ?", inviterID, constant.RoleRootUser).
		First(&inviter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	tradeNo := fmt.Sprintf("registration-blind-box-%d", userID)
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing commerceschema.BlindBoxOrder
		err := tx.Where("trade_no = ?", tradeNo).First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&commerceschema.BlindBoxOrder{
			UserId:          userID,
			Quantity:        registrationBlindBoxQuantity,
			Money:           0,
			TradeNo:         tradeNo,
			PaymentMethod:   "registration_bonus",
			PaymentProvider: "system",
			Source:          commerceschema.BlindBoxOrderSourceRegistrationBenefit,
			BenefitCycle:    fmt.Sprintf("registration:%d", userID),
			Status:          constant.TopUpStatusSuccess,
			CreateTime:      now,
			CompleteTime:    now,
			ExpiresAt:       now + int64(setting.ExpireDays)*24*60*60,
		}).Error
	})
}

func recordRegistrationBonusLog(userID int) {
	if userID <= 0 || platformconfig.QuotaForNewUser <= 0 {
		return
	}
	auditapp.RecordLog(
		userID,
		auditschema.LogTypeSystem,
		fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(platformconfig.QuotaForNewUser)),
	)
}

func applyReferralRegistrationRewards(inviterID int, inviteeID int) {
	if inviterID <= 0 || inviteeID <= 0 || !commercestore.IsPaymentComplianceConfirmed() {
		return
	}

	if err := incrementInviterAffCount(inviterID); err != nil {
		platformobservability.SysLog(fmt.Sprintf("failed to increase inviter aff_count, inviter_id=%d invitee_id=%d err=%v", inviterID, inviteeID, err))
	}
	if err := awardReferralRegisterInviteePoints(inviterID, inviteeID); err != nil {
		platformobservability.SysLog(fmt.Sprintf("failed to award referral register points, inviter_id=%d invitee_id=%d err=%v", inviterID, inviteeID, err))
	}
}

func incrementInviterAffCount(inviterID int) error {
	return platformdb.DB.Model(&identityschema.User{}).
		Where("id = ?", inviterID).
		Update("aff_count", gorm.Expr("aff_count + ?", 1)).
		Error
}

func awardReferralRegisterInviteePoints(inviterID int, inviteeID int) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if tx == nil || !tx.Migrator().HasTable(&billingschema.PointAccount{}) {
			return nil
		}
		key := fmt.Sprintf("referral-register-invitee:%d:%d", inviterID, inviteeID)
		_, _, err := billingapp.AddPointLedgerTx(
			tx,
			inviteeID,
			billingschema.PointLedgerTypeEarn,
			referralInviteeRegisterRewardPoints,
			billingschema.PointSourceReferralRegister,
			fmt.Sprintf("%d", inviterID),
			key,
			"受邀注册赠送积分",
		)
		return err
	})
}
