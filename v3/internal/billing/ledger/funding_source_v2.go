package ledger

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func optionalEconomics(e event, key string) (*int64, error) {
	if e.fields[key] == "" {
		return nil, nil
	}
	value, err := economicsNumber(e, key)
	return &value, err
}

// Facts are inserted after money/FIFO allocation in the same accepted ledger
// transaction. Unknown cost/revenue stays NULL and cannot qualify a reward.
func recordFundingSourceFactsTx(ctx context.Context, tx pgx.Tx, events []event) error {
	for _, e := range events {
		version := e.fields["funding_policy_version"]
		if version == "" && e.fields["request_procurement_cost_micro"] != "" {
			source, err := requestEconomicsBillingSource(ctx, tx, e)
			if err != nil {
				return err
			}
			kind, err := accountKindTx(ctx, tx, e.accountID)
			if err != nil {
				return err
			}
			if source == "wallet" && kind == "wallet" {
				// A newly admitted wallet-only request also freezes exact service
				// cost, even when no standard subscription exists on the account.
				copyFields := make(map[string]string, len(e.fields)+4)
				for key, value := range e.fields {
					copyFields[key] = value
				}
				e.fields = copyFields
				version = "wallet"
				e.fields["funding_policy_version"] = version
				e.fields["funding_wallet_equivalent_micro"] = e.fields["request_wallet_before_micro"]
				e.fields["funding_procurement_cost_micro"] = e.fields["request_procurement_cost_micro"]
			}
		}
		if version == "" {
			continue
		}
		if version != "legacy" && version != "standard_v2" && version != "wallet" {
			return fmt.Errorf("ledger: invalid funding policy")
		}
		equivalent, err := economicsNumber(e, "funding_wallet_equivalent_micro")
		if err != nil {
			return err
		}
		order, err := economicsNumber(e, "funding_order_id")
		if err != nil {
			return err
		}
		sub, err := economicsNumber(e, "subscription_id")
		if err != nil {
			return err
		}
		revenue, err := optionalEconomics(e, "funding_revenue_multiplier_ppm")
		if err != nil {
			return err
		}
		cost, err := optionalEconomics(e, "funding_procurement_cost_micro")
		if err != nil {
			return err
		}
		factor, err := optionalEconomics(e, billing.FieldProcurementCost)
		if err != nil {
			return err
		}
		if version == "wallet" && e.amount > 0 {
			var ppm *int64
			if err := tx.QueryRow(ctx, `SELECT floor(sum(amount::numeric*revenue_multiplier_ppm)/nullif(sum(amount),0))::bigint FROM v3_billing.funding_allocations WHERE request_id=$1 AND account_id=$2`, e.requestID, e.accountID).Scan(&ppm); err != nil {
				return err
			}
			revenue = ppm
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_billing.funding_source_usage(request_id,account_id,subscription_id,order_id,policy_version,amount,wallet_equivalent_amount,revenue_multiplier_ppm,procurement_cost_multiplier_ppm,procurement_cost_amount,settled_at)
		 VALUES($1,$2,NULLIF($3,0),NULLIF($4,0),$5,$6,$7,$8,$9,$10,$11)`, e.requestID, e.accountID, sub, order, version, e.amount, equivalent, revenue, factor, cost, fundingEventTime(e))
		if err != nil {
			return err
		}
	}
	return nil
}
