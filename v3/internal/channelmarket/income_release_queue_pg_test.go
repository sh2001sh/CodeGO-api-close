//go:build pgintegration

package channelmarket_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

type releaseQueryCapture struct {
	sql  string
	args []any
}

func (c *releaseQueryCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT id,owner_user_id,net_micro-reclaimed_micro FROM v3_channelmarket.settlements") {
		c.sql, c.args = data.SQL, append([]any(nil), data.Args...)
	}
	return ctx
}

func (*releaseQueryCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestIncomeReleaseUsesDueQueueAndSkipsLockedSettlements(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	now := time.Unix(f.now.Load(), 0)
	for _, item := range []struct {
		id    string
		owner int64
		due   time.Time
	}{
		{"oldest-owner3", 3, now.Add(-3 * time.Hour)},
		{"next-owner1", 1, now.Add(-2 * time.Hour)},
		{"last-owner1", 1, now.Add(-time.Hour)},
		{"future-owner1", 1, now.Add(time.Hour)},
	} {
		if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET owner_user_id=$2 WHERE channel_id=$1`, c.InternalChannelID, item.owner); err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
			return f.s.AccrueTx(ctx, tx, channelmarket.SettlementInput{RequestID: item.id, ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1000, GrossMicro: 1000})
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.settlements SET available_at=$2 WHERE request_id=$1`, item.id, item.due); err != nil {
			t.Fatal(err)
		}
	}
	// Released history must not be visited to fetch the next small due batch.
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.settlements
	 (id,request_id,channel_id,owner_user_id,consumer_user_id,consumer_micro,gross_micro,commission_micro,fee_micro,net_micro,status,available_at)
	 SELECT 'history:'||n,'history:'||n,$1,1,2,0,0,0,0,0,'released',$2 FROM generate_series(1,30000) n`, c.InternalChannelID, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `ANALYZE v3_channelmarket.settlements`); err != nil {
		t.Fatal(err)
	}
	capture := &releaseQueryCapture{}
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = capture
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	s := channelmarket.New(traced, nil, f.poster, channelmarket.Config{Now: func() time.Time { return now }}, nil)
	locked, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(ctx)
	if _, err = locked.Exec(ctx, `SELECT id FROM v3_channelmarket.settlements WHERE request_id='oldest-owner3' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	if result, err := s.ReleaseIncome(ctx, 1); err != nil || result.Count != 1 || result.Amount != 950 {
		t.Fatalf("skip locked release=%+v err=%v", result, err)
	}
	if f.balance(t, 1, "wallet") != 950 || f.balance(t, 3, "wallet") != 0 {
		t.Fatal("locked settlement was released or unlocked successor was skipped")
	}
	if err := locked.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if result, err := s.ReleaseIncome(ctx, 1); err != nil || result.Count != 1 || result.Amount != 950 {
		t.Fatalf("oldest release=%+v err=%v", result, err)
	}
	if f.balance(t, 3, "wallet") != 950 {
		t.Fatal("earliest due settlement starved behind another owner's history")
	}
	if result, err := s.ReleaseIncome(ctx, 100); err != nil || result.Count != 1 || result.Amount != 950 {
		t.Fatalf("remaining due release=%+v err=%v", result, err)
	}
	if result, err := s.ReleaseIncome(ctx, 100); err != nil || result.Count != 0 || result.Amount != 0 {
		t.Fatalf("future or already released settlement posted=%+v err=%v", result, err)
	}
	if f.balance(t, 1, "wallet") != 1900 || f.balance(t, 1, "marketplace_pending") != 950 || f.balance(t, 3, "marketplace_pending") != 0 {
		t.Fatal("release changed exact held/wallet amounts")
	}
	if capture.sql == "" {
		t.Fatal("release query was not observed")
	}
	// Explain the actual queue SQL. Plain EXPLAIN does not execute its row lock.
	var raw []byte
	if err := f.pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+capture.sql, capture.args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type node struct {
		Type  string `json:"Node Type"`
		Index string `json:"Index Name"`
		Plans []node `json:"Plans"`
	}
	var plans []struct{ Plan node }
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid queue plan: %v", err)
	}
	found := false
	var walk func(node)
	walk = func(n node) {
		if strings.Contains(n.Type, "Sort") {
			t.Errorf("queue sorts settlement history: %s", raw)
		}
		found = found || n.Index == "channelmarket_income_release_idx"
		for _, child := range n.Plans {
			walk(child)
		}
	}
	walk(plans[0].Plan)
	if !found {
		t.Fatalf("due queue index unused: %s", raw)
	}
	t.Log("release queue uses channelmarket_income_release_idx; locked/future/replayed settlements preserve exact balances")
}
