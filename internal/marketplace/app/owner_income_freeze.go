package app

import (
	"errors"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

// OwnerIncomeFreezeWorkflow identifies manual holds on owner income, including
// withdrawal requests waiting for administrator settlement.
const OwnerIncomeFreezeWorkflow = billingapp.MarketplaceOwnerIncomeFreezeWorkflow

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
