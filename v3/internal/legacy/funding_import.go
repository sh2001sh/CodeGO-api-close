package legacy

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func resolveFundingAccount(ctx context.Context, target pgx.Tx, p *fundingProjection) error {
	if p.account.ID == "" {
		return nil
	}
	p.values["account_id"] = nil
	owner, kind := historicalAccountMapping(p.account)
	if kind == "" {
		return nil
	}
	var id int64
	err := target.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type=$1 AND owner_id=$2 AND kind=$3`, owner, p.account.OwnerID, kind).Scan(&id)
	if err == pgx.ErrNoRows && p.remaining == 0 && p.table != "wallet_reward_holds" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("legacy: %s requires its native live account: %w", p.table, err)
	}
	p.values["account_id"] = id
	return nil
}

func (m *Importer) importFunding(ctx context.Context, target pgx.Tx, d *fundingData) error {
	for _, name := range fundingSourceNames {
		for _, row := range d.rows[name] {
			if d.retiredFundingRow(name, row) {
				continue
			}
			projected, err := d.projectFunding(name, row)
			if err != nil {
				return fmt.Errorf("legacy: project funding %s: %w", name, err)
			}
			if err = resolveFundingAccount(ctx, target, &projected); err != nil {
				return err
			}
			columns := make([]string, 0, len(projected.values))
			for field := range projected.values {
				columns = append(columns, field)
			}
			sort.Strings(columns)
			values := make([]any, len(columns))
			for i, field := range columns {
				values[i] = projected.values[field]
			}
			if err = insertHistoryExact(ctx, target, "v3_billing", name, projected.key, columns, values); err != nil {
				return fmt.Errorf("legacy: import funding %s: %w", name, err)
			}
		}
	}
	return nil
}

func (m *Importer) checkFunding(ctx context.Context, target pgx.Tx, d *fundingData, report *Report) error {
	for _, name := range fundingSourceNames {
		var expected, activeSourceCount int64
		for _, row := range d.rows[name] {
			if d.retiredFundingRow(name, row) {
				continue
			}
			activeSourceCount++
			projected, err := d.projectFunding(name, row)
			if err != nil {
				return err
			}
			if err = resolveFundingAccount(ctx, target, &projected); err != nil {
				return err
			}
			match, err := checkProjection(ctx, target, "v3_billing."+name, projected.values)
			if err != nil {
				return err
			}
			if !match {
				checkIssue(report, "billing_"+name, 0, "native funding origin, amount, reference or transfer hold differs from source")
			} else {
				expected++
			}
		}
		report.Counts["verified_billing_"+name] = expected
		if activeSourceCount > 0 {
			var targetCount int64
			if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{"v3_billing", name}.Sanitize()).Scan(&targetCount); err != nil {
				return err
			}
			report.Counts["target_billing_"+name] = targetCount
			if targetCount != activeSourceCount {
				checkIssue(report, "billing_"+name, 0, "native funding record count differs from source; check before enabling target writers")
			}
		}
	}
	return nil
}
