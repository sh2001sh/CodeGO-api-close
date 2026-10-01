//go:build pgintegration

package billing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

var ctx = context.Background()

const account = 42

// 1 credit / 1M input tokens, 2 credits / 1M output tokens, group multiplier 1.
var snap = &catalog.Snapshot{
	Groups: map[string]catalog.Group{"default": {Name: "default", Multiplier: 1}},
	Prices: map[string]catalog.Price{"gpt": {Model: "gpt", Mode: "per_token", InputPerMTok: 1_000_000, OutputPerMTok: 2_000_000}},
}

type fixedAccounts struct{}

func (fixedAccounts) WalletAccount(context.Context, int64) (int64, error) { return account, nil }

// loader returns a fixed ledger balance, like the M2 ledger reader. It is
// slow on purpose so concurrent first requests really do race the install.
type loader struct {
	balance int64
	calls   atomic.Int64
}

func (l *loader) LedgerBalance(context.Context, int64) (credits.Micro, int64, error) {
	l.calls.Add(1)
	time.Sleep(5 * time.Millisecond)
	return credits.Micro(l.balance), 1, nil
}

type clock struct{ ms atomic.Int64 }

func (c *clock) now() time.Time { return time.UnixMilli(c.ms.Load()) }

func setup(t *testing.T, balance int64) (*Settler, *redisx.Client, *loader, *clock) {
	t.Helper()
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr, PoolSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	l := &loader{balance: balance}
	c := &clock{}
	c.ms.Store(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC).UnixMilli())
	s, err := New(rdb, func() *catalog.Snapshot { return snap }, fixedAccounts{}, l,
		Config{Now: c.now}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return s, rdb, l, c
}

// body is 32 bytes: 8 prompt tokens + 100 output tokens = 8 + 200 = 208 micro-credits.
const body = `{"model":"gpt","max_tokens":100}`

func newReq(id string) *gateway.Request {
	return &gateway.Request{ID: id, Model: "gpt", Body: []byte(body), Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}}
}

func completed(prompt, completion int64) gateway.Outcome {
	return gateway.Outcome{Terminal: gateway.TerminalCompleted, Delivered: true, Charge: true,
		Usage:  gateway.Usage{PromptTokens: prompt, CompletionTokens: completion},
		Target: &gateway.Target{ChannelID: 3, CredentialID: 300}}
}

func balance(t *testing.T, rdb *redisx.Client) (bal, reserved int64) {
	t.Helper()
	v, err := rdb.HMGet(ctx, keysFor(account, "").balance, "balance", "reserved").Result()
	if err != nil {
		t.Fatal(err)
	}
	bal, _ = strconv.ParseInt(v[0].(string), 10, 64)
	reserved, _ = strconv.ParseInt(v[1].(string), 10, 64)
	return bal, reserved
}

func events(t *testing.T, rdb *redisx.Client) []map[string]any {
	t.Helper()
	msgs, err := rdb.XRange(ctx, redisx.StreamBillingEvents, "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		out[i] = m.Values
	}
	return out
}

func TestReserveSettleHappyPath(t *testing.T) {
	s, rdb, l, _ := setup(t, 1_000_000)
	req := newReq("r1")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if l.calls.Load() != 1 {
		t.Fatal("balance was not loaded on first use")
	}
	if b, r := balance(t, rdb); b != 1_000_000 || r != 208 {
		t.Fatalf("after reserve balance=%d reserved=%d; want 1000000/208", b, r)
	}
	if err := s.Finalize(ctx, req, completed(10, 20)); err != nil { // 10 + 40 = 50
		t.Fatal(err)
	}
	if b, r := balance(t, rdb); b != 999_950 || r != 0 {
		t.Fatalf("after settle balance=%d reserved=%d; want 999950/0", b, r)
	}
	ev := events(t, rdb)
	want := map[string]any{FieldRequestID: "r1", FieldAccountID: "42", FieldAmount: "50", FieldReserved: "208",
		FieldTerminal: "completed", FieldChannelID: "3", FieldCredentialID: "300", FieldPromptTokens: "10",
		FieldOutputTokens: "20", FieldEstimated: "0", FieldOverdraft: "0", FieldBalanceLoaded: "1", FieldUserID: "7", FieldKeyID: "70"}
	if len(ev) != 1 {
		t.Fatalf("stream has %d entries; want 1", len(ev))
	}
	for k, v := range want {
		if ev[0][k] != v {
			t.Errorf("event %s = %v; want %v", k, ev[0][k], v)
		}
	}
}

func TestReserveAndFinalizeAreIdempotent(t *testing.T) {
	s, rdb, _, _ := setup(t, 1_000_000)
	req := newReq("r1")
	for i := 0; i < 3; i++ {
		if err := s.Reserve(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if _, r := balance(t, rdb); r != 208 {
		t.Fatalf("reserved = %d after 3 reserves; want 208", r)
	}
	for i := 0; i < 3; i++ {
		if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := balance(t, rdb); b != 999_950 || len(events(t, rdb)) != 1 {
		t.Fatalf("balance=%d events=%d after 3 finalizes; want one charge", b, len(events(t, rdb)))
	}
	if err := s.Reserve(ctx, req); err == nil {
		t.Fatal("reserve after finalize must fail, not hold credits again")
	}
}

func TestReleaseAndInsufficient(t *testing.T) {
	s, rdb, _, _ := setup(t, 300)
	req := newReq("r1")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, newReq("r2")); !errors.Is(err, gateway.ErrInsufficientCredits) {
		t.Fatalf("second hold of 208 on 300-208 left = %v; want insufficient", err)
	}
	out := gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorBeforeOutput}
	if err := s.Finalize(ctx, req, out); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, rdb); b != 300 || r != 0 {
		t.Fatalf("release changed balance=%d reserved=%d", b, r)
	}
	if ev := events(t, rdb); len(ev) != 1 || ev[0][FieldAmount] != "0" || ev[0][FieldTerminal] != "upstream_error_before_output" {
		t.Fatalf("release event = %v", ev)
	}
	if err := s.Finalize(ctx, newReq("never-reserved"), completed(1, 1)); err != nil {
		t.Fatal("finalize without a reservation must be a no-op")
	}
}

func TestOverdraftIsChargedAndFlagged(t *testing.T) {
	s, rdb, _, _ := setup(t, 250)
	req := newReq("r1")
	if err := s.Reserve(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, req, completed(100, 500)); err != nil { // 100 + 1000 = 1100 > 250
		t.Fatal(err)
	}
	if b, _ := balance(t, rdb); b != 250-1100 {
		t.Fatalf("balance = %d; want the full charge applied (-850)", b)
	}
	if ev := events(t, rdb); ev[0][FieldOverdraft] != "1" || ev[0][FieldAmount] != "1100" {
		t.Fatalf("overdraft event = %v", ev[0])
	}
}

func TestSweepReleasesExpiredButLateFinalizeStillCharges(t *testing.T) {
	s, rdb, _, c := setup(t, 1_000_000)
	stuck, alive := newReq("stuck"), newReq("alive")
	if err := s.Reserve(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	c.ms.Add((10 * time.Minute).Milliseconds())
	if err := s.Reserve(ctx, alive); err != nil {
		t.Fatal(err)
	}
	c.ms.Add((30 * time.Minute).Milliseconds()) // stuck is 40 min old, alive 30 min
	n, err := s.SweepExpired(ctx, 100)
	if err != nil || n != 1 {
		t.Fatalf("swept %d, %v; want exactly the expired one", n, err)
	}
	if _, r := balance(t, rdb); r != 208 {
		t.Fatalf("reserved = %d; want only alive's hold", r)
	}
	if err := s.Finalize(ctx, stuck, completed(10, 20)); err != nil {
		t.Fatal(err)
	}
	if b, r := balance(t, rdb); b != 999_950 || r != 208 {
		t.Fatalf("late finalize balance=%d reserved=%d; want charge 50 without touching alive's hold", b, r)
	}
	ev := events(t, rdb)
	if len(ev) != 2 || ev[0][FieldTerminal] != "reservation_expired" || ev[1][FieldReserved] != "0" {
		t.Fatalf("events = %v", ev)
	}
}
