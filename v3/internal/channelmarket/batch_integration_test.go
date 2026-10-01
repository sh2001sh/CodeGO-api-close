//go:build pgintegration

package channelmarket_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestBatchRequiresDurableChargeAndNeverReplays(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	var wallet int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind,balance) VALUES('user',2,'wallet',1000) RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	s := channelmarket.New(f.pool, nil, f.poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, BatchRelay: func(call context.Context, r channelmarket.BatchRelayRequest) (channelmarket.BatchReceipt, error) {
		calls.Add(1)
		err := pgx.BeginFunc(call, f.pool, func(tx pgx.Tx) error {
			if _, e := f.poster.PostTx(call, tx, billing.Entry{AccountID: wallet, Amount: -20, Kind: "usage", OperationID: r.RequestID, RequestID: r.RequestID, Reason: "charged test"}); e != nil {
				return e
			}
			_, e := tx.Exec(call, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES(now(),$1,$2,$3,20,$4,$5,'success')`, wallet, r.UserID, c.InternalChannelID, r.RequestID, r.Model)
			return e
		})
		return channelmarket.BatchReceipt{RequestID: r.RequestID, AmountMicro: 999999, LogCreated: true}, err
	}}, nil)
	v, err := s.StartBatch(ctx, 2, channelmarket.BatchRequest{GroupIDs: []string{c.GroupID}, Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Batch(ctx, 3, v.ID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("foreign view: %v", err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ProcessBatchTests(ctx, 10); failures <- e }()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	v, err = s.Batch(ctx, 2, v.ID)
	if err != nil || v.Status != "completed" || len(v.Items) != 1 || v.Items[0].Status != "passed" || v.Items[0].AmountMicro != 20 || !v.LogCreated {
		t.Fatalf("receipt %+v %v", v, err)
	}
	if calls.Load() != 1 || f.balance(t, 2, "wallet") != 980 {
		t.Fatalf("duplicate charge calls=%d wallet=%d", calls.Load(), f.balance(t, 2, "wallet"))
	}
	if _, err = s.ProcessBatchTests(ctx, 10); err != nil || calls.Load() != 1 {
		t.Fatalf("replayed %v", err)
	}
}

func TestBatchRejectsAccessRevocationAndForgedSuccess(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	var calls atomic.Int64
	s := channelmarket.New(f.pool, nil, f.poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, BatchRelay: func(_ context.Context, r channelmarket.BatchRelayRequest) (channelmarket.BatchReceipt, error) {
		calls.Add(1)
		return channelmarket.BatchReceipt{RequestID: r.RequestID, AmountMicro: 500, LogCreated: true}, nil
	}}, nil)
	for _, input := range []channelmarket.BatchRequest{{Model: "fixture-model"}, {GroupIDs: []string{c.GroupID, c.GroupID}, Model: "fixture-model"}, {GroupIDs: []string{c.GroupID}, Model: "missing"}, {GroupIDs: []string{"official:restricted"}, Model: "fixture-model"}} {
		if _, err := s.StartBatch(ctx, 2, input); err == nil {
			t.Fatalf("accepted invalid %+v", input)
		}
	}
	v, err := s.StartBatch(ctx, 2, channelmarket.BatchRequest{GroupIDs: []string{c.GroupID}, Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProcessBatchTests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	v, err = s.Batch(ctx, 2, v.ID)
	if err != nil || v.Items[0].Status != "failed" || v.LogCreated || v.QuotaCharged {
		t.Fatalf("forged receipt %+v %v", v, err)
	}
	blocked, err := s.StartBatch(ctx, 2, channelmarket.BatchRequest{GroupIDs: []string{c.GroupID}, Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProcessBatchTests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	blocked, err = s.Batch(ctx, 2, blocked.ID)
	if err != nil || blocked.Items[0].Status != "failed" || calls.Load() != 1 {
		t.Fatalf("revocation ignored %+v calls=%d %v", blocked, calls.Load(), err)
	}
	if _, err = f.s.StartBatch(ctx, 2, channelmarket.BatchRequest{GroupIDs: []string{c.GroupID}, Model: "fixture-model"}); !errors.Is(err, channelmarket.ErrUnavailable) {
		t.Fatalf("nil relay: %v", err)
	}
	var wallet int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES(now(),$1,2,$2,17,$3,'fixture-model','completed')`, wallet, c.InternalChannelID, v.Items[0].RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProcessBatchTests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	v, err = s.Batch(ctx, 2, v.ID)
	if err != nil || !v.LogCreated || v.Items[0].AmountMicro != 17 || v.Items[0].Status != "failed" || calls.Load() != 1 {
		t.Fatalf("late charge unreconciled/replayed %+v %v", v, err)
	}
}

func TestPoolPrivacyAndPrivateMemberRevocation(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "private")
	f.active(t, c)
	p, err := f.s.SavePool(ctx, 1, channelmarket.RoutePool{Name: "Mine", Strategy: "cost", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}})
	if err != nil {
		t.Fatal(err)
	}
	if pools, e := f.s.Pools(ctx, 2); e != nil || len(pools) != 0 {
		t.Fatalf("pool leak %+v %v", pools, e)
	}
	p.Name = "Stolen"
	if _, err = f.s.SavePool(ctx, 2, p); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("foreign edit %v", err)
	}
	if _, err = f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Unauthorized", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}}); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("private member %v", err)
	}
	if _, err = f.s.SavePool(ctx, 1, channelmarket.RoutePool{Name: "OfficialDenied", Members: []channelmarket.PoolMember{{GroupID: "official:restricted"}}}); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("official member %v", err)
	}
}
