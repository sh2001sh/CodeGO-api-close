package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type sampledGrantRows struct {
	pgx.Rows
	raw    []json.RawMessage
	pos    int
	closed bool
}

func (r *sampledGrantRows) Next() bool { r.pos++; return r.pos <= len(r.raw) }
func (r *sampledGrantRows) Scan(dest ...any) error {
	*dest[0].(*json.RawMessage) = r.raw[r.pos-1]
	return nil
}
func (r *sampledGrantRows) Close()     { r.closed = true }
func (r *sampledGrantRows) Err() error { return nil }

type sampledGrantTx struct {
	pgx.Tx
	rows *sampledGrantRows
	err  error
	args []any
}

func (tx *sampledGrantTx) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	tx.args = args
	return tx.rows, tx.err
}

func TestSampledRenewableGrantKeepsCycleBoundaryAndPropagatesReadFailure(t *testing.T) {
	cycle := &commerceBonusCycle{subs: map[int64]commerceRow{9: commerceTestRow(t, `{"id":9,"start_time":1700000000}`)}, bonuses: map[int64]*big.Int{}, oldMarkers: map[int64][]string{9: {":member:2:"}}, accountSubs: map[string]int64{"sub9": 9}}
	rows := &sampledGrantRows{raw: []json.RawMessage{
		json.RawMessage(`{"account_id":"sub9","reason_code":"subscription_bonus","idempotency_key":"subscription-bonus:9:group-buy:1:member:2:tier:37","amount":37,"created_at":"2023-11-14T22:15:00.999999Z"}`),
		json.RawMessage(`{"account_id":"sub9","reason_code":"subscription_bonus","idempotency_key":"subscription-bonus:9:group-buy:1:member:2:tier:38","amount":38,"created_at":"2023-11-14T22:15:01Z"}`),
	}}
	tx := &sampledGrantTx{rows: rows}
	ctx := context.WithValue(context.Background(), onlineContextKey{}, &onlineView{sampled: true})
	if err := cycle.walkDelayedGrants(ctx, tx, "billing.ledger_entries", 1700000100); err != nil {
		t.Fatal(err)
	}
	if !rows.closed || cycle.bonuses[9].Int64() != 74 || tx.args[0] != "sub9" || !tx.args[1].(time.Time).Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("cycle boundary/account changed: %+v %+v", cycle.bonuses, tx.args)
	}
	want := errors.New("source unavailable")
	tx.err = want
	if err := cycle.walkDelayedGrants(ctx, tx, "billing.ledger_entries", 1700000100); !errors.Is(err, want) {
		t.Fatalf("read failure swallowed: %v", err)
	}
}
