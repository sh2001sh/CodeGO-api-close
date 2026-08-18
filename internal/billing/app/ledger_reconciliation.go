package app

import (
	"context"
	"fmt"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

type LedgerReconciliation struct {
	AccountID  string                               `json:"account_id"`
	Actual     billingschema.BillingBalanceSnapshot `json:"actual"`
	Expected   billingschema.BillingBalanceSnapshot `json:"expected"`
	Consistent bool                                 `json:"consistent"`
}

func ListLedgerReconciliations(ctx context.Context, limit int) ([]LedgerReconciliation, error) {
	if platformdb.DB == nil {
		return nil, fmt.Errorf("primary database is not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var accounts []billingschema.BillingAccount
	if err := platformdb.DB.WithContext(ctx).Order("updated_at desc").Limit(limit).Find(&accounts).Error; err != nil {
		return nil, err
	}
	results := make([]LedgerReconciliation, 0, len(accounts))
	for _, account := range accounts {
		actual, expected, err := ledgerReconciliationForAccount(ctx, account.AccountID)
		if err != nil {
			return nil, err
		}
		results = append(results, LedgerReconciliation{AccountID: account.AccountID, Actual: actual, Expected: expected, Consistent: !snapshotDiffers(actual, expected)})
	}
	return results, nil
}

// CountLedgerInconsistencies checks every billing account without repairing it.
// It is intended for cutover gates and must remain read-only.
func CountLedgerInconsistencies(ctx context.Context) (int, error) {
	if platformdb.DB == nil {
		return 0, fmt.Errorf("primary database is not initialized")
	}
	var count int64
	err := platformdb.DB.WithContext(ctx).Raw(
		ledgerInconsistencyCountQuery(),
		billingschema.BillingSettlementStatusCompleted,
		billingschema.BillingReservationStatusOpen,
	).Scan(&count).Error
	return int(count), err
}

func ledgerInconsistencyCountQuery() string {
	accountTable := billingschema.BillingAccount{}.TableName()
	snapshotTable := billingschema.BillingBalanceSnapshot{}.TableName()
	entryTable := billingschema.BillingLedgerEntry{}.TableName()
	reservationTable := billingschema.BillingReservation{}.TableName()
	settlementTable := billingschema.BillingSettlement{}.TableName()
	return fmt.Sprintf(`
WITH entry_totals AS (
  SELECT account_id,
    COALESCE(SUM(CASE WHEN entry_type IN ('grant_credit', 'reserve_release', 'settle_credit') THEN amount WHEN entry_type IN ('reserve_hold', 'settle_debit') THEN -amount ELSE 0 END), 0) AS available_balance,
    COALESCE(SUM(CASE WHEN entry_type = 'grant_credit' THEN amount ELSE 0 END), 0) AS granted_total
  FROM %s GROUP BY account_id
), settlement_totals AS (
  SELECT r.account_id, COALESCE(SUM(s.actual_amount), 0) AS consumed_total,
    COALESCE(SUM(CASE WHEN s.delta_amount < 0 THEN -s.delta_amount ELSE 0 END), 0) AS refunded_total
  FROM %s s JOIN %s r ON r.reservation_id = s.reservation_id
  WHERE s.status = ? GROUP BY r.account_id
), reservation_totals AS (
  SELECT account_id, COALESCE(SUM(reserved_amount), 0) AS reserved_balance
  FROM %s WHERE status = ? GROUP BY account_id
)
SELECT COUNT(*) FROM %s a
LEFT JOIN %s bs ON bs.account_id = a.account_id
LEFT JOIN entry_totals e ON e.account_id = a.account_id
LEFT JOIN settlement_totals s ON s.account_id = a.account_id
LEFT JOIN reservation_totals r ON r.account_id = a.account_id
WHERE bs.account_id IS NULL
  OR bs.available_balance <> COALESCE(e.available_balance, 0)
  OR bs.reserved_balance <> COALESCE(r.reserved_balance, 0)
  OR bs.granted_total <> COALESCE(e.granted_total, 0)
  OR bs.consumed_total <> COALESCE(s.consumed_total, 0)
  OR bs.refunded_total <> COALESCE(s.refunded_total, 0)`,
		entryTable, settlementTable, reservationTable, reservationTable, accountTable, snapshotTable)
}

func RepairLedgerSnapshot(ctx context.Context, accountID string) error {
	if accountID == "" {
		return fmt.Errorf("account id is required")
	}
	return platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return rebuildBalanceSnapshotTx(tx, accountID)
	})
}

func ledgerReconciliationForAccount(ctx context.Context, accountID string) (billingschema.BillingBalanceSnapshot, billingschema.BillingBalanceSnapshot, error) {
	var actual billingschema.BillingBalanceSnapshot
	if err := platformdb.DB.WithContext(ctx).Where("account_id = ?", accountID).First(&actual).Error; err != nil {
		return actual, billingschema.BillingBalanceSnapshot{}, err
	}
	expected, err := aggregateExpectedBalanceSnapshot(platformdb.DB.WithContext(ctx), accountID)
	if err != nil {
		return actual, expected, err
	}
	return actual, expected, nil
}
