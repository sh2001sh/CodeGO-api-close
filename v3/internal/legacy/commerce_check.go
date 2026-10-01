package legacy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// checkCommerce is for the offline acceptance window, before new transactions
// change imported records. Every original typed field is compared. Passwords,
// provider payloads, invoice details and redemption hashes never enter reports.
func (m *Importer) checkCommerce(ctx context.Context, target pgx.Tx, data *commerceData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	var checked int64
	for _, name := range commerceSourceNames {
		for _, source := range data.rows[name] {
			row, err := data.project(name, source)
			if err != nil {
				return err
			}
			primary := "id"
			if row.table == "wallet_transfer_security" {
				primary = "user_id"
			}
			matches, err := checkProjection(ctx, target, "v3_commerce."+row.table, row.values)
			if err != nil {
				return fmt.Errorf("legacy: check commerce %s: %w", row.table, err)
			}
			id := row.values[primary].(int64)
			if !matches {
				report.Issues = append(report.Issues, Issue{name, id, "commerce_projection_mismatch", "missing row or typed fields differ from offline source"})
			}
			checked++
			if name == "user_subscriptions" {
				amount, err := data.subscriptionOpening(source, row)
				if err != nil {
					return err
				}
				var openingMatches bool
				operation := fmt.Sprintf("v2-import:subscription:%d:subscription", id)
				if amount == 0 {
					err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.accounts a JOIN v3_commerce.subscriptions s ON s.account_id=a.id
						WHERE s.id=$1 AND a.owner_type='subscription' AND a.owner_id=$1 AND a.kind='subscription')`, id).Scan(&openingMatches)
				} else {
					err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.ledger_entries e JOIN v3_billing.accounts a ON a.id=e.account_id
						JOIN v3_commerce.subscriptions s ON s.account_id=a.id WHERE s.id=$1 AND a.owner_type='subscription' AND a.owner_id=$1
						AND a.kind='subscription' AND e.operation_id=$2 AND e.amount=$3 AND e.balance_after=$3)`, id, operation, amount).Scan(&openingMatches)
				}
				if err != nil {
					return err
				}
				if !openingMatches {
					report.Issues = append(report.Issues, Issue{name, id, "subscription_opening_mismatch", "subscription account link or immutable opening amount differs"})
				}
				var bucketMatches bool
				if err = target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_buckets b JOIN v3_billing.accounts a ON a.id=b.account_id
				 WHERE b.subscription_id=$1 AND b.starts_at=$2 AND a.owner_type='subscription' AND a.owner_id=$1 AND a.kind='subscription')`, id, row.values["starts_at"]).Scan(&bucketMatches); err != nil {
					return err
				}
				if !bucketMatches {
					checkIssue(report, name, id, "subscription admission bucket link differs")
				}
			}
		}
	}
	report.Counts["check:commerce"] = checked
	return m.checkRefundOrigins(ctx, target, data, report)
}
