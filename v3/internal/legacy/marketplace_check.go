package legacy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) checkMarketplace(ctx context.Context, target pgx.Tx, data *marketplaceData, report *Report) error {
	if report.Counts == nil {
		report.Counts = make(map[string]int64)
	}
	for _, r := range data.records {
		fields := make(map[string]any, len(r.Fields))
		for key, value := range r.Fields {
			fields[key] = value
		}
		if r.Table == "group_buy_members" {
			fields["account_id"] = nil
			if r.Fields["subscription_id"] != nil {
				var account int64
				err := target.QueryRow(ctx, `SELECT a.id FROM v3_billing.accounts a WHERE a.owner_type='subscription' AND a.owner_id=$1 AND a.kind='subscription'`, r.Fields["subscription_id"]).Scan(&account)
				if err != nil {
					report.Issues = append(report.Issues, Issue{r.Table, r.ID, "marketplace_check_missing_account", "subscription account is missing"})
					continue
				}
				fields["account_id"] = account
			}
		}
		found, err := checkProjection(ctx, target, "v3_marketplace."+r.Table, fields)
		if err != nil {
			return fmt.Errorf("legacy: marketplace check %s %d: %w", r.Table, r.ID, err)
		}
		if !found {
			checkIssue(report, r.Table, r.ID, "mapped source row is missing or at least one typed field differs")
		}
		report.Counts["check:marketplace"]++
	}
	for _, r := range data.source["balance_blind_box_items"] {
		if id := data.integer(r, "open_record_id"); id > 0 {
			var found bool
			if err := target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_marketplace.blind_box_items WHERE id=$1 AND open_record_id=$2)`, r.id(), id).Scan(&found); err != nil {
				return err
			}
			if !found {
				report.Issues = append(report.Issues, Issue{"blind_box_item", r.id(), "marketplace_check_open_link", "inventory no longer links its source open record"})
			}
		}
	}
	return nil
}
