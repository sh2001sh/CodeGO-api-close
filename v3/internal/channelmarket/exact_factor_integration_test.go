//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

func assertExactFactor(t *testing.T, actual, expected string) {
	t.Helper()
	if cmp, err := exactfactor.Compare(actual, expected); err != nil || cmp != 0 {
		t.Fatalf("exact factor=%q; want %q (%v)", actual, expected, err)
	}
}

func TestExactMarketBargainNoticeAndSettlementReplay(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	owner := channelmarket.Actor{UserID: 1}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.bargain_requests(id,group_id,user_id,proposed_ppm,reason) VALUES('tiny-bargain',$1,2,1e-57,'historical exact offer')`, c.GroupID); err != nil {
		t.Fatal(err)
	}
	bargains, err := f.s.Bargains(ctx, owner)
	if err != nil || len(bargains) != 1 {
		t.Fatalf("bargains=%v/%v", bargains, err)
	}
	assertExactFactor(t, string(bargains[0].ProposedPPM), "1e-57")
	assertExactFactor(t, string(bargains[0].Proposed), "1e-63")
	if err := f.s.ResolveBargain(ctx, owner, "tiny-bargain", true, ""); err != nil {
		t.Fatal(err)
	}
	multipliers, err := f.s.Multipliers(ctx, owner)
	if err != nil || len(multipliers) != 1 {
		t.Fatalf("multipliers=%v/%v", multipliers, err)
	}
	assertExactFactor(t, string(multipliers[0].MultiplierPPM), "1e-57")
	notices, err := f.s.Notices(ctx, 2)
	if err != nil || len(notices) != 1 || notices[0].Cleared {
		t.Fatalf("tiny offer cleared or notice missing: %v/%v", notices, err)
	}
	assertExactFactor(t, string(notices[0].MultiplierPPM), "1e-57")
	wire, err := json.Marshal(notices)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []channelmarket.Notice
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	assertExactFactor(t, string(decoded[0].MultiplierPPM), "1e-57")
	fields := map[string]string{billing.FieldRequestID: "exact-single", billing.FieldModel: "fixture-model", billing.FieldUserID: "2", billing.FieldChannelID: strconv.FormatInt(c.InternalChannelID, 10), billing.FieldAmount: "90", "marketplace_gross_micro": "90", "marketplace_multiplier_ppm": "1e-14", "billing_source": "wallet"}
	post := func(batch bool) error {
		return pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
			if batch {
				return f.s.AccrueUsageBatchTx(ctx, tx, []map[string]string{fields}, nil)
			}
			return f.s.AccrueUsageTx(ctx, tx, fields)
		})
	}
	for _, batch := range []bool{false, true} {
		fields[billing.FieldRequestID] = "exact-single"
		if batch {
			fields[billing.FieldRequestID] = "exact-batch"
		}
		fields["marketplace_multiplier_ppm"] = "1e-14"
		if err := post(batch); err != nil {
			t.Fatal(err)
		}
		fields["marketplace_multiplier_ppm"] = "0.0000000000000100"
		if err := post(batch); err != nil {
			t.Fatalf("equivalent exact replay: %v", err)
		}
		var stored string
		if err := f.pool.QueryRow(ctx, `SELECT multiplier_ppm::text FROM v3_channelmarket.settlements WHERE request_id=$1`, fields[billing.FieldRequestID]).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		assertExactFactor(t, stored, "1e-14")
		fields["marketplace_multiplier_ppm"] = "0"
		if err := post(batch); !errors.Is(err, channelmarket.ErrConflict) {
			t.Fatalf("tiny-positive settlement replayed as free: %v", err)
		}
	}
}

func TestExactMarketTimeWindowReadAndOverridePriority(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	now := time.Unix(f.now.Load(), 0)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.time_range_multipliers(id,channel_id,starts_at,ends_at,multiplier_ppm,label) VALUES('tiny-window',$1,$2,$3,1e-14,'historical exact window')`, c.InternalChannelID, now.Add(-time.Minute), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	windows, err := f.s.TimeMultipliers(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID)
	if err != nil || len(windows) != 1 {
		t.Fatalf("windows=%v/%v", windows, err)
	}
	assertExactFactor(t, string(windows[0].MultiplierPPM), "1e-14")
	assertExactFactor(t, string(windows[0].Multiplier), "1e-20")
	crypto, _ := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	snapshot, err := catalog.Compile(ctx, f.pool, crypto)
	if err != nil {
		t.Fatal(err)
	}
	assertExactFactor(t, snapshot.Market.Channels[c.InternalChannelID].FactorExact(2, now), "1e-14")
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.user_multipliers(channel_id,user_id,multiplier_ppm) VALUES($1,2,0.01)`, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = catalog.Compile(ctx, f.pool, crypto)
	if err != nil {
		t.Fatal(err)
	}
	assertExactFactor(t, snapshot.Market.Channels[c.InternalChannelID].FactorExact(2, now), "0.01")
	assertExactFactor(t, snapshot.Market.Channels[c.InternalChannelID].FactorExact(3, now), "1e-14")
	if _, err := f.s.SaveTimeMultiplier(ctx, channelmarket.Actor{UserID: 1}, channelmarket.TimeMultiplier{ChannelID: c.InternalChannelID, Start: now.Unix(), End: now.Add(time.Hour).Unix(), Multiplier: json.Number("0")}); !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("native zero window bypassed validation: %v", err)
	}
}

func TestLegacyZeroMarketResumeAndDeletedGates(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	owner, admin := channelmarket.Actor{UserID: 1}, channelmarket.Actor{UserID: 3, Admin: true}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET multiplier_ppm=0,lifecycle_status='paused',verification_status='passed' WHERE id=$1`, c.GroupID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Transition(ctx, owner, c.InternalChannelID, "resume", ""); err != nil {
		t.Fatal(err)
	}
	crypto, _ := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	snapshot, err := catalog.Compile(ctx, f.pool, crypto)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := routing.New(func() *catalog.Snapshot { return snapshot }, routing.Config{}).Plan(ctx, &gateway.Request{Model: "fixture-model", Principal: gateway.Principal{UserID: 2, Group: c.RoutingGroup}})
	if err != nil || len(plan) != 1 || plan[0].MultiplierPPMExact != "0" {
		t.Fatalf("explicit owner resume changed legacy free route: %v/%v", plan, err)
	}
	for _, state := range []string{"deleted_at", "lifecycle"} {
		t.Run(state, func(t *testing.T) {
			d := f.channel(t, "public")
			if _, err := f.s.QueueVerification(ctx, owner, d.InternalChannelID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET multiplier_ppm=0,lifecycle_status=CASE WHEN $2='lifecycle' THEN 'deleted' ELSE 'paused' END,verification_status='passed',deleted_at=CASE WHEN $2='deleted_at' THEN now() ELSE NULL END WHERE id=$1`, d.GroupID, state); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"resume", "approve"} {
				actor := owner
				if action == "approve" {
					actor = admin
				}
				if err := f.s.Transition(ctx, actor, d.InternalChannelID, action, ""); !errors.Is(err, channelmarket.ErrNotFound) {
					t.Fatalf("deleted %s accepted %s: %v", state, action, err)
				}
			}
			if _, err := f.s.QueueVerification(ctx, owner, d.InternalChannelID); !errors.Is(err, channelmarket.ErrNotFound) {
				t.Fatalf("deleted %s queued verification: %v", state, err)
			}
			if count, err := f.s.ProcessVerification(ctx, 10); err != nil || count != 0 {
				t.Fatalf("deleted %s processed queued verification: %d/%v", state, count, err)
			}
		})
	}
	if _, err := f.s.Create(ctx, 1, channelmarket.CreateRequest{Provider: "openai_compatible", BaseURL: "https://example.com", APIKey: "fixture", Models: []string{"fixture-model"}, Visibility: "public", Multiplier: json.Number("0")}); !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("new zero channel bypassed existing validation: %v", err)
	}
}

func TestVerificationDeletedDuringProbeDoesNotAcceptResult(t *testing.T) {
	for _, trigger := range []string{"manual", "auto_probe"} {
		t.Run(trigger, func(t *testing.T) {
			f := setup(t)
			c := f.channel(t, "public")
			crypto, _ := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
			s := channelmarket.New(f.pool, crypto, f.poster, channelmarket.Config{Probe: func(_ context.Context, p channelmarket.ProbeRequest) (channelmarket.ModelTest, error) {
				_, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status='deleted' WHERE id=$1`, c.GroupID)
				return channelmarket.ModelTest{Model: p.Model, Status: "passed"}, err
			}}, nil)
			run, err := s.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET trigger=$2 WHERE id=$1`, run.ID, trigger); err != nil {
				t.Fatal(err)
			}
			if n, err := s.ProcessVerification(ctx, 10); err != nil || n != 1 {
				t.Fatalf("probe=%d/%v", n, err)
			}
			var state, verification, autoStatus string
			if err := f.pool.QueryRow(ctx, `SELECT v.status,g.verification_status,coalesce(c.settings->'market'->>'auto_probe_last_status','') FROM v3_channelmarket.verification_runs v JOIN v3_channelmarket.groups g ON g.channel_id=v.channel_id JOIN v3_catalog.channels c ON c.id=v.channel_id WHERE v.id=$1`, run.ID).Scan(&state, &verification, &autoStatus); err != nil {
				t.Fatal(err)
			}
			if state != "paused" || verification == "passed" || autoStatus != "" {
				t.Fatalf("deleted probe accepted: run=%s group=%s auto=%s", state, verification, autoStatus)
			}
		})
	}
}
