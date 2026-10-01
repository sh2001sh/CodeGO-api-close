//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestAutoProbePreservesActiveChannelAndPolicy(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	c, err := f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"auto_probe_enabled":true,"auto_probe_interval_minutes":60,"auto_probe_model":"fixture-model","qps":2.5,"maintenance_window":"02:00-03:00","user_max_concurrency":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.AutoProbeEnabled || c.QPS != "2.5" || c.UserMaxConcurrency != 5 || c.Status != "active" {
		t.Fatalf("policy %+v", c)
	}
	c, err = f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"name":"Renamed"}`))
	if err != nil || !c.AutoProbeEnabled || c.QPS != "2.5" {
		t.Fatalf("partial update reset policy %+v %v", c, err)
	}
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := channelmarket.New(f.pool, crypto, f.poster, channelmarket.Config{Now: func() time.Time { return time.Unix(f.now.Load(), 0) }, Probe: func(_ context.Context, p channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
		if p.Model != "fixture-model" {
			t.Fatalf("wrong probe model %s", p.Model)
		}
		return channelmarket.ModelTest{}, errors.New("sensitive upstream credential must never escape")
	}}, nil)
	if n, e := s.QueueAutoProbes(ctx, 10); e != nil || n != 1 {
		t.Fatalf("schedule %d %v", n, e)
	}
	if n, e := s.QueueAutoProbes(ctx, 10); e != nil || n != 0 {
		t.Fatalf("duplicate schedule %d %v", n, e)
	}
	if n, e := s.ProcessVerification(ctx, 10); e != nil || n != 1 {
		t.Fatalf("process %d %v", n, e)
	}
	c, err = s.Get(ctx, channelmarket.Actor{UserID: 1}, c.ID)
	if err != nil || c.Status != "active" || c.Verification != "passed" || c.AutoProbeLastStatus != "failed" || c.AutoProbeLastAt == nil {
		t.Fatalf("auto probe withdrew channel %+v %v", c, err)
	}
	if n, e := s.QueueAutoProbes(ctx, 10); e != nil || n != 0 {
		t.Fatalf("interval ignored %d %v", n, e)
	}
	c, err = f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"name":"Again"}`))
	if err != nil || c.AutoProbeLastAt == nil || c.AutoProbeLastStatus != "failed" {
		t.Fatalf("partial update erased probe observation %+v %v", c, err)
	}
	f.now.Add(3601)
	if n, e := s.QueueAutoProbes(ctx, 10); e != nil || n != 1 {
		t.Fatalf("not rescheduled %d %v", n, e)
	}
}

func TestAutoProbeLimitDoesNotStarveLaterDueChannel(t *testing.T) {
	f := setup(t)
	first := f.channel(t, "public")
	second := f.channel(t, "public")
	f.active(t, first)
	f.active(t, second)
	for _, c := range []channelmarket.ChannelView{first, second} {
		if _, err := f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, json.RawMessage(`{"auto_probe_enabled":true,"auto_probe_interval_minutes":60}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market,auto_probe_last_at}',to_jsonb($2::timestamptz)) WHERE id=$1`, first.InternalChannelID, time.Unix(f.now.Load(), 0)); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.QueueAutoProbes(ctx, 1); err != nil || n != 1 {
		t.Fatalf("due channel starved %d %v", n, err)
	}
	var channel int64
	if err := f.pool.QueryRow(ctx, `SELECT channel_id FROM v3_channelmarket.verification_runs WHERE trigger='auto_probe' AND status='queued'`).Scan(&channel); err != nil || channel != second.InternalChannelID {
		t.Fatalf("wrong channel %d %v", channel, err)
	}
}

type failReclaimPoster struct{ base channelmarket.Poster }

func (p failReclaimPoster) PostTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (billing.PostResult, error) {
	if e.Kind == "marketplace_reclaim" {
		return billing.PostResult{}, errors.New("injected reclaim failure")
	}
	return p.base.PostTx(ctx, tx, e)
}
func TestReclaimFailurePreservesMoneyAndProgressAndCanRetry(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.accrue(t, c, "reclaim-failure")
	f.now.Add(86401)
	if _, err := f.s.ReleaseIncome(ctx, 100); err != nil {
		t.Fatal(err)
	}
	a := channelmarket.Actor{UserID: 3, Admin: true}
	input := channelmarket.ReclaimRequest{OperationID: "failed-job", OwnerIDs: []int64{1}, MaxAmount: 200}
	if _, err := f.s.QueueReclaim(ctx, a, input); err != nil {
		t.Fatal(err)
	}
	s := channelmarket.New(f.pool, nil, failReclaimPoster{f.poster}, channelmarket.Config{}, nil)
	if _, err := s.ResumeReclaims(ctx, 10); err == nil {
		t.Fatal("reclaim failure swallowed")
	}
	job, err := s.GetReclaim(ctx, a, input.OperationID)
	if err != nil || job.Status != "failed" || job.Amount != 0 || job.Count != 0 || job.Batch != 0 || f.balance(t, 1, "wallet") != 950 {
		t.Fatalf("partial failure %+v %v", job, err)
	}
	if _, err = f.s.QueueReclaim(ctx, a, input); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ResumeReclaims(ctx, 10); err != nil {
		t.Fatal(err)
	}
	job, err = f.s.GetReclaim(ctx, a, input.OperationID)
	if err != nil || job.Status != "completed" || job.Amount != 200 || f.balance(t, 1, "wallet") != 750 {
		t.Fatalf("retry %+v %v", job, err)
	}
}
