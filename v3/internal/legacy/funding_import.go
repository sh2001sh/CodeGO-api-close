package legacy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type fundingAccountKey struct {
	owner, kind string
	id          int64
}

func loadFundingAccountIndex(ctx context.Context, target pgx.Tx) (map[fundingAccountKey]int64, error) {
	rows, err := target.Query(ctx, `SELECT id,owner_type,owner_id,kind FROM v3_billing.accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := map[fundingAccountKey]int64{}
	for rows.Next() {
		var key fundingAccountKey
		var id int64
		if err := rows.Scan(&id, &key.owner, &key.id, &key.kind); err != nil {
			return nil, err
		}
		index[key] = id
	}
	return index, rows.Err()
}

func resolveFundingAccount(index map[fundingAccountKey]int64, p *fundingProjection) error {
	if p.account.ID == "" {
		return nil
	}
	p.values["account_id"] = nil
	owner, kind := historicalAccountMapping(p.account)
	if kind == "" {
		return nil
	}
	id, exists := index[fundingAccountKey{owner, kind, p.account.OwnerID}]
	if !exists {
		if p.remaining == 0 && p.table != "wallet_reward_holds" {
			return nil
		}
		return fmt.Errorf("legacy: %s requires its native live account: %w", p.table, pgx.ErrNoRows)
	}
	p.values["account_id"] = id
	return nil
}

func (d *fundingData) batches(ctx context.Context, name string, accounts map[fundingAccountKey]int64, visit func(string, []map[string]any) error) error {
	batch := make([]map[string]any, 0, exactBulkRows)
	bytes := 2 // JSON array brackets, plus a comma between projected rows.
	key := ""
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := visit(key, batch); err != nil {
			return err
		}
		clear(batch)
		batch = batch[:0]
		bytes = 2
		return nil
	}
	err := d.walk(ctx, name, func(row commerceRow) error {
		if d.retiredFundingRow(name, row) {
			return nil
		}
		p, err := d.projectFunding(name, row)
		if err != nil {
			return fmt.Errorf("legacy: project funding %s: %w", name, err)
		}
		if err = resolveFundingAccount(accounts, &p); err != nil {
			return err
		}
		encoded, err := json.Marshal(p.values)
		if err != nil {
			return fmt.Errorf("legacy: encode funding %s: %w", name, err)
		}
		if len(batch) > 0 && bytes+len(encoded)+1 > exactBulkBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		key = p.key
		if len(batch) > 0 {
			bytes++
		}
		batch = append(batch, p.values)
		bytes += len(encoded)
		if len(batch) == exactBulkRows || bytes >= exactBulkBytes {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return flush()
}

func (m *Importer) importFunding(ctx context.Context, target pgx.Tx, d *fundingData) error {
	accounts, err := loadFundingAccountIndex(ctx, target)
	if err != nil {
		return fmt.Errorf("legacy: load native funding accounts: %w", err)
	}
	for _, name := range fundingSourceNames {
		err := d.batches(ctx, name, accounts, func(key string, rows []map[string]any) error {
			return insertExactBulk(ctx, target, "v3_billing", name, []string{key}, rows)
		})
		if err != nil {
			return fmt.Errorf("legacy: import funding %s: %w", name, err)
		}
	}
	return nil
}

func (m *Importer) checkFunding(ctx context.Context, target pgx.Tx, d *fundingData, report *Report) error {
	accounts, err := loadFundingAccountIndex(ctx, target)
	if err != nil {
		return fmt.Errorf("legacy: load native funding accounts: %w", err)
	}
	for _, name := range fundingSourceNames {
		var expected, activeSourceCount int64
		emitted := false
		err := d.batches(ctx, name, accounts, func(key string, rows []map[string]any) error {
			matches, err := checkExactBulk(ctx, target, "v3_billing", name, []string{key}, rows)
			if err != nil {
				return err
			}
			if len(matches) != len(rows) {
				return fmt.Errorf("legacy: funding batch check returned %d results for %d rows", len(matches), len(rows))
			}
			activeSourceCount += int64(len(rows))
			for _, match := range matches {
				if match {
					expected++
				} else if !emitted {
					checkIssue(report, "billing_"+name, 0, "native funding origin, amount, reference or transfer hold differs from source")
					emitted = true
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("legacy: check funding %s: %w", name, err)
		}
		report.Counts["verified_billing_"+name] = expected
		if activeSourceCount > 0 {
			var targetCount int64
			query := "SELECT count(*) FROM " + pgx.Identifier{"v3_billing", name}.Sanitize()
			if name == "funding_source_policies" {
				// Migration-owned defaults do not represent a legacy valuation.
				query += " WHERE NOT (source IN ('referral_reward','subscription_conversion','blind_box_batch_base','blind_box_batch_reward') AND revenue_multiplier_ppm=0)"
			}
			if err := target.QueryRow(ctx, query).Scan(&targetCount); err != nil {
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
