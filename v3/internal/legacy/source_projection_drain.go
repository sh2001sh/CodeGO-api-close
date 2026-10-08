package legacy

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func projectionSource(sources map[string]string, prefix, name string) string {
	if table := sources[prefix+name]; table != "" {
		return table
	}
	return sources[name]
}

// Old installations can have partial projection schemas. Missing authoritative
// columns deny the additional contract; they never turn an unknown row safe.
func projectionColumns(ctx context.Context, tx pgx.Tx, table string, columns ...string) (bool, error) {
	if table == "" {
		return false, nil
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_attribute
	 WHERE attrelid=$1::regclass AND attnum>0 AND NOT attisdropped AND attname=ANY($2::text[])`, table, columns).Scan(&count)
	return count == len(columns), err
}

// executionDrainSQL has a deliberately narrow extra branch. V2 schedules this
// projection after the synchronous funding settlement. Its actual_amount and
// usage evidence are raw provider quota, whereas settlements contain the final
// funding debit after subscription/card scaling and balance caps. They are not
// interchangeable money amounts. Source records remain unchanged and archived.
func executionDrainSQL(ctx context.Context, tx pgx.Tx, sources map[string]string, alias string) (string, error) {
	e := alias
	terminal := e + `.status='settled'`
	parent := projectionSource(sources, "gateway_", "request_executions")
	r := projectionSource(sources, "billing_", "reservations")
	s := projectionSource(sources, "billing_", "settlements")
	accounts := projectionSource(sources, "billing_", "accounts")
	for _, required := range []struct {
		table   string
		columns []string
	}{
		{parent, strings.Fields("execution_id request_id user_id token_id account_id reservation_id settlement_id actual_amount usage_evidence_id")},
		{r, strings.Fields("reservation_id request_id account_id reserved_amount status")},
		{s, strings.Fields("settlement_id reservation_id actual_amount delta_amount usage_evidence_id status")},
		{accounts, strings.Fields("account_id owner_type owner_id account_type quota_unit")},
		{sources["users"], strings.Fields("id")},
		{sources["tokens"], strings.Fields("id user_id")},
	} {
		ok, err := projectionColumns(ctx, tx, required.table, required.columns...)
		if err != nil || !ok {
			return terminal, err
		}
	}
	owner := `(account.owner_type='user' AND account.owner_id=` + e + `.user_id AND account.account_type IN ('wallet','claude_wallet'))`
	if subscriptions := sources["user_subscriptions"]; subscriptions != "" {
		ok, err := projectionColumns(ctx, tx, subscriptions, "id", "user_id")
		if err != nil {
			return "", err
		}
		if ok {
			owner += ` OR (account.owner_type='user_subscription' AND account.account_type='subscription'
			 AND EXISTS(SELECT 1 FROM ` + subscriptions + ` sub WHERE sub.id=account.owner_id AND sub.user_id=` + e + `.user_id))`
		}
	}
	proof := fmt.Sprintf(`EXISTS(SELECT 1 FROM %s reservation
	 JOIN %s settlement ON settlement.settlement_id=%s.settlement_id AND settlement.reservation_id=reservation.reservation_id
	 JOIN %s account ON account.account_id=reservation.account_id
	 WHERE reservation.reservation_id=%s.reservation_id AND reservation.request_id=%s.request_id AND reservation.account_id=%s.account_id
	 AND reservation.status='settled' AND settlement.status='completed'
	 AND reservation.reserved_amount>0 AND settlement.actual_amount>=0
	 AND settlement.actual_amount::numeric=reservation.reserved_amount::numeric+settlement.delta_amount::numeric
	 AND settlement.usage_evidence_id=%s.request_id
	 AND (coalesce(%s.usage_evidence_id,'')='' OR %s.usage_evidence_id=settlement.usage_evidence_id)
	 AND account.quota_unit='quota' AND (%s)
	 AND EXISTS(SELECT 1 FROM %s user_record WHERE user_record.id=%s.user_id)
	 AND (%s.token_id=0 OR EXISTS(SELECT 1 FROM %s key_record WHERE key_record.id=%s.token_id AND key_record.user_id=%s.user_id))
	 AND NOT EXISTS(SELECT 1 FROM %s other_reservation WHERE other_reservation.request_id=%s.request_id AND coalesce(other_reservation.status,'') NOT IN ('settled','released','expired'))
	 AND NOT EXISTS(SELECT 1 FROM %s other_settlement JOIN %s other_reservation ON other_reservation.reservation_id=other_settlement.reservation_id
	 WHERE other_reservation.request_id=%s.request_id AND coalesce(other_settlement.status,'') NOT IN ('completed','rejected')))`,
		r, s, e, accounts, e, e, e, e, e, e, owner, sources["users"], e, e, sources["tokens"], e, e, r, e, s, r, e)
	// Subscription top-ups create additional reservations. The execution's
	// settlement_id references only the original slice, not the request total.
	fundingTotal := `(SELECT coalesce(sum(funding_settlement.actual_amount),0) FROM ` + s + ` funding_settlement JOIN ` + r + ` funding_reservation ON funding_reservation.reservation_id=funding_settlement.reservation_id
	 WHERE funding_reservation.request_id=` + e + `.request_id AND funding_reservation.account_id=` + e + `.account_id AND funding_reservation.status='settled' AND funding_settlement.status='completed')`
	proof += ` AND (EXISTS(SELECT 1 FROM ` + accounts + ` funding_account WHERE funding_account.account_id=` + e + `.account_id AND funding_account.account_type='subscription') OR ` + e + `.actual_amount::numeric>=` + fundingTotal + `)`
	proof += ` AND NOT EXISTS(SELECT 1 FROM ` + r + ` funding_reservation LEFT JOIN ` + s + ` funding_settlement ON funding_settlement.reservation_id=funding_reservation.reservation_id
	 WHERE funding_reservation.request_id=` + e + `.request_id AND funding_reservation.account_id=` + e + `.account_id AND funding_reservation.status='settled'
	 AND (funding_settlement.status IS DISTINCT FROM 'completed' OR funding_settlement.usage_evidence_id IS DISTINCT FROM ` + e + `.request_id
	 OR funding_settlement.actual_amount<0 OR funding_settlement.actual_amount::numeric IS DISTINCT FROM funding_reservation.reserved_amount::numeric+funding_settlement.delta_amount::numeric))`
	// A completed audit is a funding-debit projection, and a present raw usage
	// projection must match its execution. Missing optional projections do not
	// erase the authoritative settlement. Conflicting present evidence does deny.
	if audits := projectionSource(sources, "gateway_", "request_audits"); audits != "" {
		ok, err := projectionColumns(ctx, tx, audits, "request_id", "user_id", "token_id", "billable", "quota", "status")
		if err != nil {
			return "", err
		}
		if ok {
			proof += ` AND NOT EXISTS(SELECT 1 FROM ` + audits + ` audit WHERE audit.request_id=` + e + `.request_id
			 AND (audit.user_id IS DISTINCT FROM ` + e + `.user_id OR audit.token_id IS DISTINCT FROM ` + e + `.token_id OR (audit.status IS DISTINCT FROM 'in_flight' AND
			 (audit.billable IS DISTINCT FROM true OR audit.quota::numeric IS DISTINCT FROM ` + fundingTotal + `))))`
			if logs := sources["logs"]; logs != "" {
				logsOK, err := projectionColumns(ctx, tx, logs, "request_id", "user_id", "token_id", "type", "quota")
				if err != nil {
					return "", err
				}
				if logsOK {
					proof += ` AND (EXISTS(SELECT 1 FROM ` + audits + ` audit WHERE audit.request_id=` + e + `.request_id)
					 OR ((SELECT count(*) FROM ` + logs + ` consumption WHERE consumption.request_id=` + e + `.request_id AND consumption.type=2)=1
					 AND EXISTS(SELECT 1 FROM ` + logs + ` consumption WHERE consumption.request_id=` + e + `.request_id AND consumption.type=2 AND consumption.user_id=` + e + `.user_id AND consumption.token_id=` + e + `.token_id AND consumption.quota::numeric=` + fundingTotal + `)))`
				} else {
					proof += ` AND EXISTS(SELECT 1 FROM ` + audits + ` audit WHERE audit.request_id=` + e + `.request_id)`
				}
			} else {
				proof += ` AND EXISTS(SELECT 1 FROM ` + audits + ` audit WHERE audit.request_id=` + e + `.request_id)`
			}
		}
	}
	if usage := projectionSource(sources, "gateway_", "usage_evidence"); usage != "" {
		ok, err := projectionColumns(ctx, tx, usage, "request_id", "execution_id", "actual_amount")
		if err != nil {
			return "", err
		}
		if ok {
			proof += ` AND NOT EXISTS(SELECT 1 FROM ` + usage + ` evidence WHERE evidence.request_id=` + e + `.request_id
			 AND (evidence.execution_id IS DISTINCT FROM ` + e + `.execution_id OR evidence.actual_amount IS DISTINCT FROM ` + e + `.actual_amount))`
		}
	}
	return `(` + terminal + ` OR (` + e + `.status IN ('recorded','provider_completed') AND ` + e + `.actual_amount>=0 AND ` + proof + `))`, nil
}

// A missing HTTP result stays unknown. This predicate proves that the request
// has no future funding/provider work; it does not claim a successful response.
type projectionDrainCondition struct {
	predicate string
	ctes      string
}

func auditDrainSQL(ctx context.Context, tx pgx.Tx, sources map[string]string, alias string) (projectionDrainCondition, error) {
	a := alias
	terminal := a + `.status IN ('succeeded','failed','rejected','cancelled')`
	r := projectionSource(sources, "billing_", "reservations")
	s := projectionSource(sources, "billing_", "settlements")
	accounts := projectionSource(sources, "billing_", "accounts")
	for _, required := range []struct {
		table   string
		columns []string
	}{
		{r, strings.Fields("reservation_id request_id account_id reserved_amount status")},
		{s, strings.Fields("reservation_id actual_amount delta_amount usage_evidence_id status")},
		{accounts, strings.Fields("account_id owner_type owner_id account_type quota_unit")},
	} {
		ok, err := projectionColumns(ctx, tx, required.table, required.columns...)
		if err != nil || !ok {
			return projectionDrainCondition{predicate: terminal}, err
		}
	}
	owner := `(account.owner_type='user' AND account.owner_id=` + a + `.user_id AND account.account_type IN ('wallet','claude_wallet'))
	 OR (account.owner_type='token' AND account.owner_id=` + a + `.token_id AND account.account_type='token')`
	if sub := sources["user_subscriptions"]; sub != "" {
		ok, err := projectionColumns(ctx, tx, sub, "id", "user_id")
		if err != nil {
			return projectionDrainCondition{}, err
		}
		if ok {
			owner += ` OR (account.owner_type='user_subscription' AND account.account_type='subscription' AND EXISTS(SELECT 1 FROM ` + sub + ` subscription WHERE subscription.id=account.owner_id AND subscription.user_id=` + a + `.user_id))`
		}
	}
	// Every reservation for this request must have a matching owner and either
	// completed settlement or explicit release. Expired alone is insufficient.
	canonical := `EXISTS(SELECT 1 FROM ` + r + ` reservation WHERE reservation.request_id=` + a + `.request_id)
	 AND NOT EXISTS(SELECT 1 FROM ` + r + ` reservation LEFT JOIN ` + accounts + ` account ON account.account_id=reservation.account_id
	 WHERE reservation.request_id=` + a + `.request_id AND (account.account_id IS NULL OR account.quota_unit IS DISTINCT FROM 'quota' OR (` + owner + `) IS NOT TRUE
	 OR NOT ((reservation.status='released' AND NOT EXISTS(SELECT 1 FROM ` + s + ` settlement WHERE settlement.reservation_id=reservation.reservation_id AND settlement.status IS DISTINCT FROM 'rejected')) OR (reservation.status='settled' AND EXISTS(SELECT 1 FROM ` + s + ` settlement
	 WHERE settlement.reservation_id=reservation.reservation_id AND settlement.status='completed' AND settlement.actual_amount>=0
	 AND settlement.actual_amount::numeric=reservation.reserved_amount::numeric+settlement.delta_amount::numeric AND settlement.usage_evidence_id=` + a + `.request_id)))))
	 AND NOT EXISTS(SELECT 1 FROM ` + s + ` settlement JOIN ` + r + ` reservation ON reservation.reservation_id=settlement.reservation_id
	 WHERE reservation.request_id=` + a + `.request_id AND coalesce(settlement.status,'') NOT IN ('completed','rejected'))`
	noMoney := `NOT EXISTS(SELECT 1 FROM ` + r + ` reservation WHERE reservation.request_id=` + a + `.request_id)`
	for _, name := range []string{"request_executions", "usage_evidence"} {
		if table := projectionSource(sources, "gateway_", name); table != "" {
			ok, err := projectionColumns(ctx, tx, table, "request_id")
			if err != nil {
				return projectionDrainCondition{}, err
			}
			if !ok {
				noMoney += " AND FALSE"
			} else {
				noMoney += ` AND NOT EXISTS(SELECT 1 FROM ` + table + ` other_projection WHERE other_projection.request_id=` + a + `.request_id)`
			}
		}
	}
	finalEvidence := `FALSE`
	if logs := sources["logs"]; logs != "" {
		ok, err := projectionColumns(ctx, tx, logs, "request_id", "user_id", "token_id", "type", "other")
		if err != nil {
			return projectionDrainCondition{}, err
		}
		if ok {
			// Other is old text JSON. A malformed matching record raises a visible
			// validation error and blocks migration, including on PostgreSQL 15.
			finalEvidence += ` OR EXISTS(SELECT 1 FROM ` + logs + ` failure WHERE failure.request_id=` + a + `.request_id AND failure.user_id=` + a + `.user_id AND failure.token_id=` + a + `.token_id
			 AND failure.type=5 AND
			 failure.other::jsonb @> '{"status":"failed"}'::jsonb AND coalesce(failure.other::jsonb->>'is_channel_test','false')='false'
			 AND jsonb_typeof(failure.other::jsonb->'counted_in_success_rate')='boolean'
			 AND jsonb_typeof(failure.other::jsonb->'status_code')='number' AND (failure.other::jsonb->>'status_code')~'^[45][0-9]{2}$'
			 AND jsonb_typeof(failure.other::jsonb->'retry_count')='number' AND (failure.other::jsonb->>'retry_count')~'^[0-9]+$'
			 AND coalesce(failure.other::jsonb->>'error_code','')<>'' AND coalesce(failure.other::jsonb->>'error_type','')<>''
			 AND coalesce(failure.other::jsonb->>'request_path','')~'^/(v1/|v1beta/)')`
			noMoney += ` AND NOT EXISTS(SELECT 1 FROM ` + logs + ` consumption WHERE consumption.request_id=` + a + `.request_id AND consumption.type=2)`
		}
	}
	if background := projectionSource(sources, "gateway_", "responses_background_jobs"); background != "" {
		ok, err := projectionColumns(ctx, tx, background, "id", "user_id", "token_id", "status")
		if err != nil {
			return projectionDrainCondition{}, err
		}
		if ok {
			finalEvidence += ` OR EXISTS(SELECT 1 FROM ` + background + ` background WHERE background.id=` + a + `.request_id AND background.user_id=` + a + `.user_id AND background.token_id=` + a + `.token_id AND background.status IN ('completed','failed','cancelled'))`
			backgroundConflict := ` AND NOT EXISTS(SELECT 1 FROM ` + background + ` background WHERE background.id=` + a + `.request_id AND (coalesce(background.status,'') NOT IN ('completed','failed','cancelled') OR background.user_id IS DISTINCT FROM ` + a + `.user_id OR background.token_id IS DISTINCT FROM ` + a + `.token_id))`
			canonical += backgroundConflict
			noMoney += backgroundConflict
		} else {
			// A present but unknown background shape cannot prove no pending work.
			canonical += ` AND FALSE`
			noMoney += ` AND FALSE`
		}
	}
	if economics := projectionSource(sources, "billing_", "request_economics"); economics != "" {
		ok, err := projectionColumns(ctx, tx, economics, "request_id")
		if err != nil {
			return projectionDrainCondition{}, err
		}
		if ok {
			noMoney += ` AND NOT EXISTS(SELECT 1 FROM ` + economics + ` economics WHERE economics.request_id=` + a + `.request_id)`
		}
	}
	identity := "FALSE"
	usersOK, err := projectionColumns(ctx, tx, sources["users"], "id")
	if err != nil {
		return projectionDrainCondition{}, err
	}
	keysOK, err := projectionColumns(ctx, tx, sources["tokens"], "id", "user_id")
	if err != nil {
		return projectionDrainCondition{}, err
	}
	if usersOK && keysOK {
		identity = `EXISTS(SELECT 1 FROM ` + sources["users"] + ` user_record WHERE user_record.id=` + a + `.user_id)
		 AND (` + a + `.token_id=0 OR EXISTS(SELECT 1 FROM ` + sources["tokens"] + ` key_record WHERE key_record.id=` + a + `.token_id AND key_record.user_id=` + a + `.user_id))`
	}
	noObligation, noFinancial, ctes, err := auditNoObligationSQL(ctx, tx, sources, a)
	if err != nil {
		return projectionDrainCondition{}, err
	}
	return projectionDrainCondition{predicate: `(` + terminal + ` OR (` + a + `.status='in_flight' AND (` + identity + `) AND ((` + canonical + `) OR (` + noMoney + ` AND (((` + finalEvidence + `) AND (` + noFinancial + `)) OR (` + noObligation + `))))))`, ctes: ctes}, nil
}

func validateProjectionDrains(ctx context.Context, source pgx.Tx, sources map[string]string, report *Report) error {
	for _, item := range []struct {
		name      string
		predicate func(context.Context, pgx.Tx, map[string]string, string) (projectionDrainCondition, error)
	}{
		{"request_executions", func(ctx context.Context, tx pgx.Tx, sources map[string]string, alias string) (projectionDrainCondition, error) {
			predicate, err := executionDrainSQL(ctx, tx, sources, alias)
			return projectionDrainCondition{predicate: predicate}, err
		}}, {"request_audits", auditDrainSQL},
	} {
		table := projectionSource(sources, "gateway_", item.name)
		if table == "" {
			continue
		}
		condition, err := item.predicate(ctx, source, sources, "projection")
		if err != nil {
			return err
		}
		var pending, archived int64
		ctes := condition.ctes
		if ctes != "" {
			ctes += ", "
		}
		if err = source.QueryRow(ctx, `WITH `+ctes+` projection_classification AS MATERIALIZED
		 (SELECT projection.status,(`+condition.predicate+`) drained FROM `+table+` projection)
		 SELECT count(*) FILTER(WHERE NOT coalesce(drained,false)),
		 count(*) FILTER(WHERE drained AND status IN ('recorded','provider_completed','in_flight')) FROM projection_classification`).Scan(&pending, &archived); err != nil {
			return fmt.Errorf("legacy: inspect %s canonical drain: %w", item.name, err)
		}
		report.Counts["in_flight:gateway_"+item.name] = pending
		report.Counts["archived_projection_diagnostics.gateway_"+item.name] = archived
		if pending > 0 {
			report.Issues = append(report.Issues, Issue{"gateway_" + item.name, 0, "source_work_pending", "projection has conflicting or unproven funding/provider obligations; reconcile source work before import"})
		}
	}
	return nil
}
