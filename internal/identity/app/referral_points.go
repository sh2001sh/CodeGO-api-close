package app

import (
	"fmt"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"

	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"gorm.io/gorm"
)

func insertUserAndApplyRegistrationRewards(user *identityschema.User, inviterID int) error {
	if user == nil {
		return fmt.Errorf("user is nil")
	}
	if err := identitystore.CreateUser(user, inviterID); err != nil {
		return err
	}
	recordRegistrationBonusLog(user.Id)
	applyReferralRegistrationRewards(inviterID, user.Id)
	return nil
}

func finalizeOAuthUserAndApplyRegistrationRewards(user *identityschema.User, inviterID int, grantBlindBoxes bool) {
	if user == nil {
		return
	}
	if err := identitystore.FinalizeCreatedUser(user.Id); err != nil {
		platformobservability.SysLog(fmt.Sprintf("failed to finalize created user %d: %v", user.Id, err))
	}
	if grantBlindBoxes {
		if err := grantRegistrationBlindBoxes(user.Id); err != nil {
			platformobservability.SysLog(fmt.Sprintf("failed to grant registration blind boxes to user %d: %v", user.Id, err))
		}
	}
	recordRegistrationBonusLog(user.Id)
	applyReferralRegistrationRewards(inviterID, user.Id)
}

func grantRegistrationBlindBoxes(userID int) error {
	// Registration no longer grants monetary lottery inventory. Keep the legacy
	// entry point inert so older OAuth callers cannot revive the campaign.
	return nil
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
}

func incrementInviterAffCount(inviterID int) error {
	return platformdb.DB.Model(&identityschema.User{}).
		Where("id = ?", inviterID).
		Update("aff_count", gorm.Expr("aff_count + ?", 1)).
		Error
}
