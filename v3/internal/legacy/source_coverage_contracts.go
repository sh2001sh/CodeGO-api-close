package legacy

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// These records have no native writer or pending financial work to resume.
// Their exact evidence remains in the frozen source backup, not a new grant.
// Community resources and stored simulation sessions were retired in v2;
// setups records old bootstrap metadata, not permissions for the new service.
func validateArchivedSourceContracts(ctx context.Context, source pgx.Tx, sources map[string]string, known map[string]bool, report *Report) error {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	for _, name := range strings.Fields(`balance_blind_box_simulation_batches balance_blind_box_simulation_sessions
		community_resources setups wallet_quota_conversions`) {
		table := sources[name]
		if table == "" {
			continue
		}
		var count int64
		if err := source.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		known[table] = true
		report.Counts["archived_source_history."+name] = count
		if name == "wallet_quota_conversions" {
			// v2 committed each dual-wallet conversion and its audit atomically.
			// A different state is not a completed historical conversion.
			var pending int64
			if err := source.QueryRow(ctx, `SELECT count(*) FROM `+table+` t
				WHERE COALESCE(to_jsonb(t)->>'status','')<>'completed'`).Scan(&pending); err != nil {
				return err
			}
			report.Counts["in_flight:wallet_quota_conversions"] = pending
			if pending > 0 {
				report.Issues = append(report.Issues, Issue{name, 0, "source_work_pending", "only completed old dual-wallet conversion audits may remain in the frozen source"})
			}
		}
	}
	return validateArchivedExecutionEvidence(ctx, source, sources, known, report)
}

// v2's Temporal settlement projections are not routing configuration. Native
// settlement and audit history comes from the imported ledger/log/audit facts.
// Retain the detailed old execution tree after proving its parent's funding
// terminal, even when its post-settlement projection never finished.
func validateArchivedExecutionEvidence(ctx context.Context, source pgx.Tx, sources map[string]string, known map[string]bool, report *Report) error {
	parent := sources["gateway_request_executions"]
	if parent == "" {
		parent = sources["request_executions"]
	}
	parentDrained := "FALSE"
	if parent != "" {
		var err error
		parentDrained, err = executionDrainSQL(ctx, source, sources, "execution")
		if err != nil {
			return err
		}
	}
	for _, name := range []string{"execution_attempts", "route_plans", "usage_evidence"} {
		table := sources["gateway_"+name]
		if table == "" {
			table = sources[name]
		}
		if table == "" {
			continue
		}
		var count int64
		if err := source.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		known[table] = true
		report.Counts["archived_source_history.gateway_"+name] = count
		if count == 0 {
			continue
		}
		if parent == "" {
			report.Issues = append(report.Issues, Issue{"gateway_" + name, 0, "unproven_execution_evidence", "execution evidence requires its settled request_executions source"})
			continue
		}
		field := "execution_id"
		if name == "route_plans" {
			field = "route_plan_id"
		}
		linked := ""
		if name != "execution_attempts" {
			linked = " AND child.request_id IS NOT DISTINCT FROM execution.request_id"
		}
		if name == "usage_evidence" {
			full, err := projectionColumns(ctx, source, parent, "actual_amount")
			if err != nil {
				return err
			}
			if full {
				linked += " AND child.actual_amount IS NOT DISTINCT FROM execution.actual_amount"
			}
		}
		var unsettled int64
		if err := source.QueryRow(ctx, `SELECT count(*) FROM `+table+` child
			WHERE COALESCE(child.`+field+`,'')='' OR NOT EXISTS(
			SELECT 1 FROM `+parent+` execution WHERE execution.`+field+`=child.`+field+`
			AND (`+parentDrained+`)`+linked+`)`).Scan(&unsettled); err != nil {
			return err
		}
		report.Counts["unproven_execution_evidence.gateway_"+name] = unsettled
		if unsettled > 0 {
			report.Issues = append(report.Issues, Issue{"gateway_" + name, 0, "unproven_execution_evidence", "execution evidence has a missing identifier, absent parent or no proven terminal funding; drain and reconcile the source first"})
		}
	}
	return nil
}
