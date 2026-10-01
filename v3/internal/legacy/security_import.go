package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

func (d *securityData) walk(ctx context.Context, name string, visit func(map[string]any) error) error {
	return walkHistory(ctx, d.source, d.sources[name], func(raw json.RawMessage) error {
		if name == "security_audit_events" {
			a, err := decodeSecurityAudit(raw)
			if err != nil {
				return err
			}
			return visit(a.projection())
		}
		s, err := decodeSecurityState(raw)
		if err != nil {
			return err
		}
		return visit(s.projection())
	})
}

func (m *Importer) importSecurityData(ctx context.Context, target pgx.Tx, d *securityData) error {
	for _, name := range []string{"account_request_abuse_states", "security_audit_events"} {
		key := "id"
		if name == "account_request_abuse_states" {
			key = "user_id"
		}
		if err := d.walk(ctx, name, func(fields map[string]any) error {
			columns := make([]string, 0, len(fields))
			for column := range fields {
				columns = append(columns, column)
			}
			sort.Strings(columns)
			values := make([]any, len(columns))
			for i, column := range columns {
				values[i] = fields[column]
			}
			return insertHistoryExact(ctx, target, "v3_security", name, key, columns, values)
		}); err != nil {
			return fmt.Errorf("legacy: import retained security %s: %w", name, err)
		}
	}
	report := Report{}
	if err := m.checkSecurityData(ctx, target, d, &report); err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return fmt.Errorf("legacy: imported security rows failed exact reconciliation")
	}
	return nil
}

func (m *Importer) checkSecurityData(ctx context.Context, target pgx.Tx, d *securityData, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	for _, name := range []string{"account_request_abuse_states", "security_audit_events"} {
		if err := d.walk(ctx, name, func(fields map[string]any) error {
			equal, err := checkProjection(ctx, target, "v3_security."+name, fields)
			if err != nil {
				return err
			}
			if !equal {
				if name == "account_request_abuse_states" {
					checkIssue(report, name, fields["user_id"].(int64), "retained security state differs from original persistent fields")
				} else {
					checkIssue(report, name, 0, fmt.Sprintf("retained security audit %s differs from original persistent fields", fields["id"]))
				}
			}
			report.Counts["check:security"]++
			return nil
		}); err != nil {
			return err
		}
		var count int64
		if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{"v3_security", name}.Sanitize()).Scan(&count); err != nil {
			return err
		}
		report.Counts["check:security:"+name+":actual"] = count
		if count != d.counts[name] {
			checkIssue(report, name, 0, fmt.Sprintf("security row count source=%d target=%d", d.counts[name], count))
		}
	}
	return nil
}
