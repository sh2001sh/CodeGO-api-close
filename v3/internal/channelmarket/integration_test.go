//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/migrations"
)

var ctx = context.Background()

type fixture struct {
	pool   *pgxpool.Pool
	s      *channelmarket.Service
	poster *ledger.Poster
	now    atomic.Int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		if _, err = pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		sql, e := migrations.Read(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, sql); e != nil {
			t.Fatalf("apply %s: %v", file, e)
		}
	}
	for i := int64(1); i <= 3; i++ {
		if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES($1,$2)`, i, fmt.Sprintf("market-user-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{pool: pool, poster: ledger.NewPoster(pool)}
	f.now.Store(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC).Unix())
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	f.s = channelmarket.New(pool, crypto, f.poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, Probe: func(_ context.Context, p channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
		return channelmarket.ModelTest{Model: p.Model, Status: "passed"}, nil
	}}, nil)
	return f
}
func (f *fixture) channel(t *testing.T, visibility string) channelmarket.ChannelView {
	t.Helper()
	c, err := f.s.Create(ctx, 1, channelmarket.CreateRequest{Provider: "openai_compatible", BaseURL: "https://example.com", APIKey: "fixture-channel-secret", Models: []string{"fixture-model"}, Visibility: visibility, Multiplier: json.Number("0.1")})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *fixture) active(t *testing.T, c channelmarket.ChannelView) {
	t.Helper()
	if _, err := f.s.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.ProcessVerification(ctx, 10); err != nil || n != 1 {
		t.Fatalf("probe %d %v", n, err)
	}
	if err := f.s.Transition(ctx, channelmarket.Actor{UserID: 3, Admin: true}, c.InternalChannelID, "approve", ""); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) balance(t *testing.T, owner int64, kind string) int64 {
	t.Helper()
	var amount int64
	if err := f.pool.QueryRow(ctx, `SELECT coalesce((SELECT balance FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind=$2),0)`, owner, kind).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	return amount
}
func (f *fixture) accrue(t *testing.T, c channelmarket.ChannelView, id string) {
	t.Helper()
	err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		return f.s.AccrueTx(ctx, tx, channelmarket.SettlementInput{RequestID: id, ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1000, GrossMicro: 1000, MultiplierPPM: 100000})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrivateChannelAccessBlocksAndSnapshot(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "private")
	f.active(t, c)
	if _, err := f.s.Get(ctx, channelmarket.Actor{UserID: 2}, c.PublicSlug); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("private leaked: %v", err)
	}
	if err := f.s.SetBlock(ctx, channelmarket.Actor{UserID: 2}, c.InternalChannelID, 3, true); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("other owner changed blocks: %v", err)
	}
	invite, err := f.s.CreateInvite(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInvite(ctx, 2, invite.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Get(ctx, channelmarket.Actor{UserID: 2}, c.PublicSlug); err != nil {
		t.Fatal(err)
	}
	value := json.Number("0.025")
	if err = f.s.SetMultiplier(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, 2, &value); err != nil {
		t.Fatal(err)
	}
	if err = f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		snap, e := catalog.ReadMarketSnapshot(ctx, tx)
		if e != nil {
			return e
		}
		cp := snap.Channels[c.InternalChannelID]
		if !cp.Blocked[2] || cp.Factor(2, time.Now()) != 25000 {
			return errors.New("snapshot lost block/multiplier")
		}
		if !snap.Groups[cp.GroupName].Allows(2) || snap.Groups[cp.GroupName].Allows(3) {
			return errors.New("snapshot lost private ACL")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.BindToken(ctx, 2, c.GroupID, 100); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("blocked key bind: %v", err)
	}
	var secret []byte
	if err = f.pool.QueryRow(ctx, `SELECT secret FROM v3_catalog.channel_credentials WHERE channel_id=$1`, c.InternalChannelID).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(secret, []byte("fixture-channel-secret")) {
		t.Fatal("unencrypted secret")
	}
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("fixture-channel-secret")) || bytes.Contains(body, []byte("example.com")) {
		t.Fatal("private upstream data leaked")
	}
}

func TestConcurrentIncomeReleaseAndIdempotentReclaim(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.accrue(t, c, "request-1")
	f.accrue(t, c, "request-1")
	if got := f.balance(t, 1, "marketplace_pending"); got != 950 {
		t.Fatalf("duplicate accrual: %d", got)
	}
	err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		return f.s.AccrueTx(ctx, tx, channelmarket.SettlementInput{RequestID: "request-1", ChannelID: c.InternalChannelID, ConsumerUserID: 2, ConsumerMicro: 1001, GrossMicro: 1000, MultiplierPPM: 100000})
	})
	if !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("altered accrual accepted: %v", err)
	}
	if r, err := f.s.ReleaseIncome(ctx, 100); err != nil || r.Count != 0 {
		t.Fatalf("hold not enforced: %+v %v", r, err)
	}
	f.now.Add(86401)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.s.ReleaseIncome(ctx, 100); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := f.balance(t, 1, "wallet"); got != 950 {
		t.Fatalf("release duplicated: %d", got)
	}
	if got := f.balance(t, 1, "marketplace_pending"); got != 0 {
		t.Fatalf("held account not drained: %d", got)
	}
	r := channelmarket.ReclaimRequest{OperationID: "reclaim-1", OwnerIDs: []int64{1}, MaxAmount: 200}
	if _, err = f.s.QueueReclaim(ctx, channelmarket.Actor{UserID: 2}, r); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("non-admin reclaim: %v", err)
	}
	for i := 0; i < 2; i++ {
		_, e := f.s.QueueReclaim(ctx, channelmarket.Actor{UserID: 3, Admin: true}, r)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.ResumeReclaims(ctx, 10); e != nil {
			t.Fatal(e)
		}
		got, e := f.s.GetReclaim(ctx, channelmarket.Actor{UserID: 3, Admin: true}, r.OperationID)
		if e != nil || got.Amount != 200 || got.Status != "completed" {
			t.Fatalf("reclaim: %+v %v", got, e)
		}
	}
	if got := f.balance(t, 1, "wallet"); got != 750 {
		t.Fatalf("duplicate reclaim: %d", got)
	}
	r.MaxAmount = 201
	if _, err = f.s.QueueReclaim(ctx, channelmarket.Actor{UserID: 3, Admin: true}, r); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("altered reclaim: %v", err)
	}
}

type failPoster struct{ base *ledger.Poster }

func (p failPoster) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	if e.Kind == "marketplace_release" && e.Amount > 0 {
		return billing.PostResult{}, errors.New("injected release failure")
	}
	return p.base.PostTx(ctx, tx, e)
}
func TestReleaseTransactionFailureKeepsHeldMoney(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.accrue(t, c, "release-failure")
	f.now.Add(86401)
	broken := channelmarket.New(f.pool, nil, failPoster{f.poster}, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }}, nil)
	if _, err := broken.ReleaseIncome(ctx, 100); err == nil {
		t.Fatal("injected error swallowed")
	}
	if got := f.balance(t, 1, "marketplace_pending"); got != 950 {
		t.Fatalf("partial debit committed: %d", got)
	}
	if got := f.balance(t, 1, "wallet"); got != 0 {
		t.Fatalf("partial credit: %d", got)
	}
	if r, err := f.s.ReleaseIncome(ctx, 100); err != nil || r.Amount != 950 {
		t.Fatalf("retry: %+v %v", r, err)
	}
}
