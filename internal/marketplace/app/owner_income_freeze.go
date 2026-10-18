package app

import (
	"errors"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// OwnerIncomeFreezeWorkflow identifies administrative wallet reservations. They
// have no request ID or expiry and remain held until manual settlement/release.
const OwnerIncomeFreezeWorkflow = "marketplace_owner_income_freeze"

type OwnerIncomeFreeze struct {
	FrozenAmount int64 `json:"frozen_amount"`
}

// GetOwnerIncomeFreeze returns current wallet-wide holds, independent of income
// date/channel filters. Account ownership always comes from the authenticated user.
func GetOwnerIncomeFreeze(userID int) (OwnerIncomeFreeze, error) {
	var result OwnerIncomeFreeze
	if userID <= 0 {
		return result, errors.New("invalid owner")
	}
	err := platformdb.DB.Table(billingschema.BillingReservation{}.TableName()+" AS r").
		Select("COALESCE(SUM(r.reserved_amount), 0) AS frozen_amount").
		Joins("JOIN "+billingschema.BillingAccount{}.TableName()+" AS a ON a.account_id = r.account_id").
		Where("a.owner_type = ? AND a.owner_id = ? AND a.account_type = ? AND a.quota_unit = ?", "user", userID, "claude_wallet", "quota").
		Where("r.workflow_id = ? AND r.status = ?", OwnerIncomeFreezeWorkflow, billingschema.BillingReservationStatusOpen).
		Scan(&result).Error
	return result, err
}
