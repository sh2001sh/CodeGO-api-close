package legacy

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
)

type financialHistoryVerificationKey struct{}

func financialHistoryVerification(ctx context.Context) bool {
	value, _ := ctx.Value(financialHistoryVerificationKey{}).(bool)
	return value
}

func aggregateHistorySource(name string) bool {
	return name == "logs" || name == "request_audits" || name == "request_attempt_audits"
}

// PostgreSQL sums numeric original units before the exact factor-of-two
// conversion. Neither large JSON log payloads nor floating point cross the wire.
func loadFinancialHistorySummary(ctx context.Context, d *historyData, name, table string) error {
	if table == "" {
		return nil
	}
	window := historyWindow(ctx, name, "t", d.sources["request_audits"], d.sources["request_attempt_audits"])
	if name == "request_attempt_audits" {
		var count int64
		if err := d.source.QueryRow(ctx, "SELECT count(*) FROM "+table+" t WHERE "+window).Scan(&count); err != nil {
			return err
		}
		d.counts[name] = count
		return nil
	}
	var count, usageCount, bad int64
	var sum, usageSum string
	extra := "0::bigint,'0'::text"
	invalid := "coalesce(t.quota,0)::numeric NOT BETWEEN -4611686018427387904 AND 4611686018427387903"
	if name == "logs" {
		extra = "count(*) FILTER(WHERE t.type=2),(coalesce(sum(coalesce(t.quota,0)::numeric) FILTER(WHERE t.type=2),0)*2)::text"
		invalid += " OR (t.type=2 AND coalesce(t.quota,0)<0)"
	} else {
		invalid += " OR coalesce(t.quota,0)<0"
	}
	query := "SELECT count(*),(coalesce(sum(coalesce(t.quota,0)::numeric),0)*2)::text," + extra + ",count(*) FILTER(WHERE " + invalid + ") FROM " + table + " t WHERE " + window
	if err := d.source.QueryRow(ctx, query).Scan(&count, &sum, &usageCount, &usageSum, &bad); err != nil {
		return err
	}
	if bad != 0 {
		return errors.New("legacy: historical monetary amounts are invalid")
	}
	d.counts[name] = count
	amount, ok := new(big.Int).SetString(sum, 10)
	if !ok {
		return errors.New("legacy: invalid exact historical amount sum")
	}
	d.amounts[name] = amount
	if name == "logs" {
		d.counts["usage_logs"] = usageCount
		amount, ok = new(big.Int).SetString(usageSum, 10)
		if !ok {
			return errors.New("legacy: invalid exact usage amount sum")
		}
		d.amounts["usage_logs"] = amount
		query = "SELECT coalesce(sum(n),0)::bigint FROM(SELECT count(*) n FROM " + table + " t WHERE type=2 AND request_id<>'' AND " + window + " GROUP BY created_at,request_id,user_id HAVING count(*)>1)s"
		var duplicates int64
		if err := d.source.QueryRow(ctx, query).Scan(&duplicates); err != nil {
			return err
		}
		d.counts["usage_request_ids_disambiguated"] = duplicates
	}
	return nil
}

// Grouping by user, Key and event type prevents a same-total redistribution
// between accounts from passing. Source and target use the same RR snapshots
// as catch-up and the unchanged exact funding/market checks.
func checkFinancialHistoryGroups(ctx context.Context, source, target pgx.Tx, sources map[string]string) error {
	for _, item := range []struct {
		source, target, kind string
		usage                bool
	}{
		{"logs", "v3_audit.events", "coalesce(event_type,0)", false},
		{"logs", "v3_billing.usage_logs", "2", true},
		{"request_audits", "v3_audit.request_audits", "0", false},
	} {
		table := sources[item.source]
		if table == "" {
			continue
		}
		window := historyWindow(ctx, item.source, "t", sources["request_audits"], sources["request_attempt_audits"])
		sourceKind := "coalesce(t.type,0)"
		if item.source == "request_audits" {
			sourceKind = "0"
		}
		if item.usage {
			window = "(" + window + ") AND t.type=2"
		}
		sourceSQL := "SELECT coalesce(t.user_id,0),coalesce(t.token_id,0)," + sourceKind + ",count(*),(coalesce(sum(coalesce(t.quota,0)::numeric),0)*2)::text FROM " + table + " t WHERE " + window + " GROUP BY 1,2,3 ORDER BY 1,2,3"
		targetSQL := "SELECT coalesce(user_id,0),coalesce(key_id,0)," + item.kind + ",count(*),coalesce(sum(amount::numeric),0)::text FROM " + item.target + " GROUP BY 1,2,3 ORDER BY 1,2,3"
		left, err := source.Query(ctx, sourceSQL)
		if err != nil {
			return err
		}
		right, err := target.Query(ctx, targetSQL)
		if err != nil {
			left.Close()
			return err
		}
		err = compareFinancialHistoryGroups(left, right)
		left.Close()
		right.Close()
		if err != nil {
			return fmt.Errorf("legacy: %s financial history differs: %w", item.target, err)
		}
	}
	return nil
}

func compareFinancialHistoryGroups(left, right pgx.Rows) error {
	for left.Next() {
		if !right.Next() {
			if right.Err() != nil {
				return right.Err()
			}
			return errors.New("monetary group missing")
		}
		var a, b struct {
			user, key, kind, count int64
			sum                    string
		}
		if err := left.Scan(&a.user, &a.key, &a.kind, &a.count, &a.sum); err != nil {
			return err
		}
		if err := right.Scan(&b.user, &b.key, &b.kind, &b.count, &b.sum); err != nil {
			return err
		}
		if a != b {
			return errors.New("monetary group amount or count mismatch")
		}
	}
	if left.Err() != nil {
		return left.Err()
	}
	if right.Next() {
		return errors.New("unexpected monetary group")
	}
	return right.Err()
}
