//go:build pgintegration

package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type onlineLogNormalizationTx struct {
	pgx.Tx
	updates, maxRows, failAt int
	t                        *testing.T
	planChecks               int
}

func (tx *onlineLogNormalizationTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "WITH batch AS MATERIALIZED") {
		tx.updates++
		var batch []json.RawMessage
		if err := json.Unmarshal(args[0].([]byte), &batch); err != nil {
			return pgconn.CommandTag{}, err
		}
		if len(batch) > tx.maxRows {
			tx.maxRows = len(batch)
		}
		if tx.failAt == tx.updates {
			return tx.Tx.Exec(ctx, "SELECT 1/0")
		}
		if tx.t != nil && (tx.updates == 1 || tx.updates == 6) {
			var encoded json.RawMessage
			if err := tx.Tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&encoded); err != nil {
				return pgconn.CommandTag{}, err
			}
			var plans []map[string]any
			if err := json.Unmarshal(encoded, &plans); err != nil {
				return pgconn.CommandTag{}, err
			}
			groupIndex, usageIndex, usageTID := false, false, false
			var inspect func(map[string]any)
			inspect = func(plan map[string]any) {
				if plan["Index Name"] == "online_log_group_idx" {
					groupIndex = true
				}
				if plan["Relation Name"] == "v3_billing__usage_logs_default" {
					if plan["Node Type"] == "Seq Scan" {
						tx.t.Fatal("bounded log normalization scans the whole usage table")
					}
					condition, _ := plan["Index Cond"].(string)
					usageIndex = usageIndex || strings.Contains(condition, "created_at") && strings.Contains(condition, "id")
					usageTID = usageTID || plan["Node Type"] == "Tid Scan"
				}
				children, _ := plan["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0]["Plan"].(map[string]any))
			if !groupIndex || !usageIndex || !usageTID {
				tx.t.Fatalf("bounded log index plan group=%t usage=%t tid=%t", groupIndex, usageIndex, usageTID)
			}
			tx.planChecks++
		}
	}
	return tx.Tx.Exec(ctx, query, args...)
}

func TestOnlineFullLogNormalizationStaysBoundedUnderTempLimit(t *testing.T) {
	m, _, target, opts := onlineMigrationFixture(t, false)
	onlineMigrationReady(t, m, opts)
	ctx := context.Background()
	events, usage := onlineStage("v3_audit.events"), onlineStage("v3_billing.usage_logs")
	const size = 35000
	seed, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = seed.Rollback(ctx) }()
	if err := onlineAuthorize(ctx, seed, opts.RunID); err != nil {
		t.Fatal(err)
	}
	_, err = seed.Exec(ctx, `INSERT INTO `+events+` SELECT x.* FROM `+events+` a
	 CROSS JOIN generate_series(1,35000)g CROSS JOIN LATERAL jsonb_populate_record(NULL::`+events+`,
	 to_jsonb(a)||jsonb_build_object('id',100000+g,'user_id',CASE WHEN g=2050 THEN 8 ELSE 7 END,
	 'created_at',to_timestamp(1700020000)+CASE WHEN g=2051 THEN interval '1 microsecond' ELSE interval '0' END,
	 'event_type',CASE WHEN g=2052 THEN 6 ELSE 2 END,
	 'request_id',CASE WHEN g=2053 THEN '' WHEN g<=2052 THEN 'cross-page-duplicate' ELSE 'unique-'||g||repeat('x',200) END))x WHERE a.id=51;
	 INSERT INTO `+usage+`(id,created_at,account_id,user_id,amount,request_id) OVERRIDING SYSTEM VALUE
	 SELECT id,created_at,7,user_id,2,'pending-'||id FROM `+events+` WHERE id>100000;
	 ANALYZE `+events+`; ANALYZE `+usage)
	if err != nil {
		t.Fatal(err)
	}
	if err = seed.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	begin := func() pgx.Tx {
		t.Helper()
		tx, e := target.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if e = onlineAuthorize(ctx, tx, opts.RunID); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, "SET LOCAL work_mem='64kB'; SET LOCAL temp_file_limit='1MB'"); e != nil {
			t.Fatal(e)
		}
		return tx
	}
	// Reproduce the production failure with the previous unbounded query under
	// a scaled 1MB limit, then run the replacement under that same limit.
	old := begin()
	_, err = old.Exec(ctx, `WITH repeated AS(SELECT e.created_at,e.user_id,e.request_id,count(*) n FROM `+events+` e WHERE e.event_type=2 AND e.request_id<>'' GROUP BY e.created_at,e.user_id,e.request_id)
	 UPDATE `+usage+` u SET request_id=CASE WHEN r.n>1 THEN e.request_id||':v2-log:'||e.id ELSE e.request_id END
	 FROM `+events+` e JOIN repeated r USING(created_at,user_id,request_id)
	 WHERE u.created_at=e.created_at AND u.id=e.id AND e.event_type=2 AND u.request_id IS DISTINCT FROM CASE WHEN r.n>1 THEN e.request_id||':v2-log:'||e.id ELSE e.request_id END`)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "53400" {
		t.Fatalf("old full normalization did not reproduce temp limit: %v", err)
	}
	if err = old.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	failed := begin()
	interrupted := &onlineLogNormalizationTx{Tx: failed, failAt: 3}
	err = onlineNormalizeLogs(ctx, interrupted, nil, true)
	if !errors.As(err, &pgerr) || pgerr.Code != "22012" || interrupted.updates != 3 {
		t.Fatalf("late failure not reached: updates=%d err=%v", interrupted.updates, err)
	}
	if err = failed.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var unchanged int
	if err = target.QueryRow(ctx, "SELECT count(*) FROM "+usage+" WHERE id>100000 AND request_id='pending-'||id").Scan(&unchanged); err != nil || unchanged != size {
		t.Fatalf("late failure partially committed: unchanged=%d err=%v", unchanged, err)
	}
	write := begin()
	defer func() { _ = write.Rollback(ctx) }()
	counted := &onlineLogNormalizationTx{Tx: write, t: t}
	if err = onlineNormalizeLogs(ctx, counted, nil, true); err != nil {
		t.Fatal(err)
	}
	var checked, bad, changedMoney int
	err = write.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE u.request_id IS DISTINCT FROM CASE
	 WHEN e.id IN(102052,102053) THEN 'pending-'||e.id
	 WHEN e.id<=102049 THEN e.request_id||':v2-log:'||e.id ELSE e.request_id END),
	 count(*) FILTER(WHERE u.amount<>2) FROM `+events+` e
	 CROSS JOIN LATERAL(SELECT request_id,amount FROM `+usage+` u WHERE u.created_at=e.created_at AND u.id=e.id OFFSET 0)u
	 WHERE e.id>100000`).Scan(&checked, &bad, &changedMoney)
	if err != nil || checked != size || bad != 0 || changedMoney != 0 || counted.maxRows > exactBulkRows || counted.updates < 60 || counted.planChecks != 2 {
		t.Fatalf("bounded normalization checked=%d bad=%d money=%d updates=%d max=%d err=%v", checked, bad, changedMoney, counted.updates, counted.maxRows, err)
	}
	if err = write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("old query rejected with SQLSTATE53400; rows=%d updates=%d max_batch=%d index_plans=%d temp_file_limit=1MB; cross-page duplicate/user/microsecond/empty/nonusage boundaries and late rollback preserved", size, counted.updates, counted.maxRows, counted.planChecks)
}
