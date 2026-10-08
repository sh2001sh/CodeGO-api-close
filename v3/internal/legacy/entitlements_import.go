package legacy

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func (m *Importer) importEntitlements(ctx context.Context, target pgx.Tx, data *entitlementsData) error {
	// No live ledger postings: opening balances already include paid rewards.
	for _, contract := range entitlementContracts {
		targetTable := entitlementTargetTable(contract.source)
		for _, row := range data.rows[contract.source] {
			fields, err := entitlementProject(contract, row)
			if err != nil {
				return fmt.Errorf("legacy: project entitlement %s: %w", contract.source, err)
			}
			columns := make([]string, 0, len(fields))
			for name := range fields {
				columns = append(columns, name)
			}
			sort.Strings(columns)
			values := make([]any, len(columns))
			for i, name := range columns {
				values[i] = fields[name]
			}
			if err = insertHistoryExact(ctx, target, "v3_commerce", targetTable, "id", columns, values); err != nil {
				return fmt.Errorf("legacy: import entitlement %s ID %v: %w", contract.source, fields["id"], err)
			}
		}
		if len(data.rows[contract.source]) > 0 {
			qualified := pgx.Identifier{"v3_commerce", targetTable}.Sanitize()
			if _, err := target.Exec(ctx, `SELECT setval(pg_get_serial_sequence($1,'id'),GREATEST(COALESCE((SELECT max(id) FROM `+qualified+`),0),1),EXISTS(SELECT 1 FROM `+qualified+`))`, "v3_commerce."+targetTable); err != nil {
				return fmt.Errorf("legacy: entitlement sequence %s: %w", contract.source, err)
			}
		}
	}
	return nil
}

func (m *Importer) checkEntitlements(ctx context.Context, target pgx.Tx, data *entitlementsData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	for _, contract := range entitlementContracts {
		for _, row := range data.rows[contract.source] {
			fields, err := entitlementProject(contract, row)
			if err != nil {
				return fmt.Errorf("legacy: check entitlement projection %s: %w", contract.source, err)
			}
			equal, err := checkProjection(ctx, target, "v3_commerce."+entitlementTargetTable(contract.source), fields)
			if err != nil {
				return err
			}
			if !equal {
				checkIssue(report, contract.source, fields["id"].(int64), "native entitlement columns differ from source")
			}
			report.Counts["check:entitlements"]++
		}
		var actual int64
		if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{"v3_commerce", entitlementTargetTable(contract.source)}.Sanitize()).Scan(&actual); err != nil {
			return err
		}
		report.Counts["check:entitlements:"+contract.source+":actual"] = actual
		if actual != int64(len(data.rows[contract.source])) {
			checkIssue(report, contract.source, 0, "entitlement source and target record counts differ")
		}
	}
	return nil
}

func entitlementTargetTable(source string) string {
	if source == "subscription_claude_conversions" {
		return "subscription_value_conversions"
	}
	return source
}
