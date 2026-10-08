package legacy

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The absence proof is set-based: usage/reference reverse links have no usable
// leading index on some v2 installations. Scan each such source once, rather
// than scanning millions of financial rows for each unknown HTTP request.
func auditNoObligationSQL(ctx context.Context, tx pgx.Tx, sources map[string]string, alias string) (string, string, string, error) {
	audits := projectionSource(sources, "gateway_", "request_audits")
	reservations := projectionSource(sources, "billing_", "reservations")
	settlements := projectionSource(sources, "billing_", "settlements")
	ledger := projectionSource(sources, "billing_", "ledger_entries")
	for _, item := range []struct {
		table   string
		columns []string
	}{
		{audits, strings.Fields("request_id status billable quota")},
		{ledger, strings.Fields("reference_id idempotency_key")},
	} {
		ok, err := projectionColumns(ctx, tx, item.table, item.columns...)
		if err != nil || !ok {
			return "FALSE", "FALSE", "", err
		}
	}
	refs := []string{
		`SELECT candidate.request_id FROM ` + settlements + ` fact JOIN projection_obligation_candidates candidate ON candidate.request_id=fact.usage_evidence_id`,
		// Every reference_type is covered, including NULL and unknown old kinds.
		`SELECT candidate.request_id FROM ` + ledger + ` fact CROSS JOIN LATERAL unnest(ARRAY[fact.reference_id,` + requestOperationRequestSQL("fact.idempotency_key") + `]) financial_reference(request_id)
		 JOIN projection_obligation_candidates candidate ON candidate.request_id=financial_reference.request_id`,
	}
	for _, item := range []struct{ prefix, name, field string }{
		{"gateway_", "request_executions", "request_id"},
		{"gateway_", "usage_evidence", "request_id"},
		{"billing_", "request_economics", "request_id"},
		{"billing_", "funding_allocations", "request_id"},
		{"", "subscription_pre_consume_records", "request_id"},
		{"workflow_", "task_workflows", "request_id"},
	} {
		table := projectionSource(sources, item.prefix, item.name)
		if table == "" {
			continue
		}
		ok, err := projectionColumns(ctx, tx, table, item.field)
		if err != nil || !ok {
			return "FALSE", "FALSE", "", err
		}
		refs = append(refs, `SELECT candidate.request_id FROM `+table+` fact JOIN projection_obligation_candidates candidate ON candidate.request_id=fact.`+item.field)
	}
	noTerminalEvidence := "TRUE"
	if logs := sources["logs"]; logs != "" {
		ok, err := projectionColumns(ctx, tx, logs, "request_id", "type")
		if err != nil || !ok {
			return "FALSE", "FALSE", "", err
		}
		// A present error log must pass the separate rigorous terminal signature.
		refs = append(refs, `SELECT candidate.request_id FROM `+logs+` fact JOIN projection_obligation_candidates candidate ON candidate.request_id=fact.request_id WHERE fact.type=2`)
		noTerminalEvidence += ` AND NOT EXISTS(SELECT 1 FROM ` + logs + ` error_log WHERE error_log.request_id=` + alias + `.request_id AND error_log.type=5)`
	} else {
		return "FALSE", "FALSE", "", nil
	}
	if background := projectionSource(sources, "gateway_", "responses_background_jobs"); background != "" {
		ok, err := projectionColumns(ctx, tx, background, "id")
		if err != nil || !ok {
			return "FALSE", "FALSE", "", err
		}
		noTerminalEvidence += ` AND NOT EXISTS(SELECT 1 FROM ` + background + ` background WHERE background.id=` + alias + `.request_id)`
	}
	if tasks := sources["tasks"]; tasks != "" {
		ok, err := projectionColumns(ctx, tx, tasks, "private_data")
		if err != nil || !ok {
			return "FALSE", "FALSE", "", err
		}
		// Invalid text JSON raises a validation error; it never becomes absence.
		refs = append(refs, `SELECT candidate.request_id FROM `+tasks+` fact JOIN projection_obligation_candidates candidate ON candidate.request_id=fact.private_data::jsonb->>'request_id'`)
	}
	prefixes := requestOperationPrefixesSQL(alias)
	noPrefixes := requestOperationAbsenceSQL(ledger, prefixes)
	if outbox := projectionSource(sources, "billing_", "outbox_events"); outbox != "" {
		ok, err := projectionColumns(ctx, tx, outbox, "aggregate_id", "payload", "idempotency_key")
		if err != nil || !ok {
			return "FALSE", "FALSE", "", err
		}
		refs = append(refs, `SELECT candidate.request_id FROM `+outbox+` fact CROSS JOIN LATERAL unnest(ARRAY[fact.aggregate_id,fact.payload::jsonb->>'request_id',fact.payload::jsonb->>'reference_id',fact.payload::jsonb->>'usage_evidence_id',`+requestOperationRequestSQL(`CASE WHEN starts_with(fact.idempotency_key,'outbox:') THEN substring(fact.idempotency_key FROM 8) END`)+`]) financial_reference(request_id)
		 JOIN projection_obligation_candidates candidate ON candidate.request_id=financial_reference.request_id`)
		noPrefixes += ` AND ` + requestOperationAbsenceSQL(outbox, `ARRAY(SELECT 'outbox:'||prefix FROM unnest(`+prefixes+`) AS operation(prefix))`)
	}
	ctes := `projection_obligation_candidates AS MATERIALIZED (
	 SELECT candidate.request_id FROM ` + audits + ` candidate WHERE candidate.status='in_flight' AND candidate.billable IS FALSE AND candidate.quota=0
	 AND NOT EXISTS(SELECT 1 FROM ` + reservations + ` reservation WHERE reservation.request_id=candidate.request_id)),
	 projection_obligation_refs AS MATERIALIZED (` + strings.Join(refs, " UNION ") + `)`
	noFinancial := alias + `.billable IS FALSE AND ` + alias + `.quota=0 AND coalesce(` + alias + `.request_id,'')<>''
	 AND position(':' in ` + alias + `.request_id)=0
	 AND NOT EXISTS(SELECT 1 FROM projection_obligation_refs reference WHERE reference.request_id=` + alias + `.request_id) AND ` + noPrefixes
	return noFinancial + ` AND ` + noTerminalEvidence, noFinancial, ctes, nil
}

// The reverse scan also catches unknown/non-ASCII operation suffixes outside
// the indexed range. V2 generates colon-free request UUIDs; a colon-bearing
// unknown request cannot be proven by this operation encoding and is refused.
func requestOperationRequestSQL(key string) string {
	return `CASE WHEN starts_with(` + key + `,'entry:subscription:') THEN split_part(` + key + `,':',3)
	 WHEN starts_with(` + key + `,'subscription:') THEN split_part(` + key + `,':',2)
	 WHEN starts_with(` + key + `,'entry:relay:wallet:') OR starts_with(` + key + `,'entry:relay:claude_wallet:') OR starts_with(` + key + `,'entry:relay:subscription:') THEN split_part(` + key + `,':',4)
	 WHEN starts_with(` + key + `,'relay:wallet:') OR starts_with(` + key + `,'relay:claude_wallet:') OR starts_with(` + key + `,'relay:subscription:') THEN split_part(` + key + `,':',3) END`
}

func requestOperationPrefixesSQL(alias string) string {
	return `ARRAY['entry:subscription:'||` + alias + `.request_id||':','subscription:'||` + alias + `.request_id||':',
	 'entry:relay:wallet:'||` + alias + `.request_id||':','relay:wallet:'||` + alias + `.request_id||':',
	 'entry:relay:claude_wallet:'||` + alias + `.request_id||':','relay:claude_wallet:'||` + alias + `.request_id||':',
	 'entry:relay:subscription:'||` + alias + `.request_id||':','relay:subscription:'||` + alias + `.request_id||':']`
}

// V2 operation suffixes are ASCII reserve/settle/release names and decimal IDs.
// The bounded range uses its existing B-tree; starts_with treats request IDs
// literally (unlike LIKE), including percent/underscore characters.
func requestOperationAbsenceSQL(table, prefixes string) string {
	return `NOT EXISTS(SELECT 1 FROM unnest(` + prefixes + `) AS operation(prefix)
	 CROSS JOIN LATERAL (SELECT 1 FROM ` + table + ` financial_operation
	 WHERE financial_operation.idempotency_key>=operation.prefix AND financial_operation.idempotency_key<operation.prefix||'zzzz'
	 AND starts_with(financial_operation.idempotency_key,operation.prefix) LIMIT 1) matched)`
}
