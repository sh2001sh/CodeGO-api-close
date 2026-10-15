//go:build pgintegration

package identity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type keyPageQueryCapture struct {
	sql  string
	args []any
}

func (c *keyPageQueryCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "retired_usage_totals") {
		c.sql, c.args = data.SQL, append([]any(nil), data.Args...)
	}
	return ctx
}

func (*keyPageQueryCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestKeyPageAggregatesOnlyVisibleKeys(t *testing.T) {
	pool, _ := testDeps(t)
	for id := int64(1); id <= 120; id++ {
		seedKey(t, pool, 1, id)
	}
	mustExec(t, pool, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('user',1,'wallet',1000000)`)
	mustExec(t, pool, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,key_id,amount,request_id,model)
	 SELECT now(),(SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=1 AND kind='wallet'),
	 1,1+(n-1)%120,1,'page-usage-'||n,'mock' FROM generate_series(1,240) n`)
	mustExec(t, pool, `INSERT INTO v3_billing.retired_usage_totals(user_id,key_id,amount) SELECT 1,n,3 FROM generate_series(1,120) n`)
	capture := &keyPageQueryCapture{}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = capture
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	control := &Control{pool: traced}
	page, err := control.keyPage(ctx, 1, 11, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 120 || len(page.Items) != 10 || page.Items[0].ID != 20 || page.Items[9].ID != 11 {
		t.Fatalf("incorrect key page: total=%d items=%v", page.Total, page.Items)
	}
	for _, key := range page.Items {
		if key.SpentMicroCredits != 5 {
			t.Fatalf("key %d lost live/retired consumption: %d", key.ID, key.SpentMicroCredits)
		}
	}
	if capture.sql == "" {
		t.Fatal("key page SQL was not observed")
	}
	// Explain the actual query issued by keyPage, rather than a mirrored SQL
	// fixture. The old plan aggregates 110 keys to return this 10-key page.
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE,FORMAT JSON) "+capture.sql, capture.args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		Type  string     `json:"Node Type"`
		Loops int        `json:"Actual Loops"`
		Plans []planNode `json:"Plans"`
	}
	var decoded []struct{ Plan planNode }
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("invalid execution plan: %v", err)
	}
	aggregates := 0
	var walk func(planNode)
	walk = func(node planNode) {
		if node.Type == "Aggregate" {
			aggregates++
			t.Logf("usage aggregate actual loops=%d visible keys=%d", node.Loops, len(page.Items))
			if node.Loops != len(page.Items) {
				t.Errorf("usage aggregated %d keys for a %d-key page", node.Loops, len(page.Items))
			}
		}
		for _, child := range node.Plans {
			walk(child)
		}
	}
	walk(decoded[0].Plan)
	if aggregates == 0 {
		t.Fatal("usage aggregation missing from the execution plan")
	}
	empty, err := control.keyPage(ctx, 1, 99, 10)
	if err != nil || empty.Total != 120 || len(empty.Items) != 0 {
		t.Fatalf("empty page changed count/items: total=%d count=%d err=%v", empty.Total, len(empty.Items), err)
	}
}
