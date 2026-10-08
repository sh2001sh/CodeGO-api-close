//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/catalogcontrol"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func TestPausedChannelResumesOriginalGroupAndPool(t *testing.T) {
	for _, via := range []string{"owner", "catalog", "tag"} {
		t.Run(via, func(t *testing.T) {
			f := setup(t)
			c := f.channel(t, "private")
			f.active(t, c)
			owner, consumer := channelmarket.Actor{UserID: 1}, channelmarket.Actor{UserID: 2}
			invite, err := f.s.CreateInvite(ctx, owner, c.InternalChannelID, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.AcceptInvite(ctx, consumer.UserID, invite.Token); err != nil {
				t.Fatal(err)
			}
			var key int64
			if err = f.pool.QueryRow(ctx, `INSERT INTO v3_identity.api_keys(user_id,name,key_hash,key_prefix,key_ciphertext) VALUES(2,'retained-key',$1,'resume',$2) RETURNING id`, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 48)).Scan(&key); err != nil {
				t.Fatal(err)
			}
			if err = f.s.BindToken(ctx, consumer.UserID, c.GroupID, key); err != nil {
				t.Fatal(err)
			}
			if err = f.s.SaveGroupFavorite(ctx, consumer, c.GroupID, true); err != nil {
				t.Fatal(err)
			}
			pool, err := f.s.SavePool(ctx, consumer.UserID, channelmarket.RoutePool{Name: "Retained routes", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}})
			if err != nil {
				t.Fatal(err)
			}
			crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
			if err != nil {
				t.Fatal(err)
			}
			var current *catalog.Snapshot
			planner := routing.New(func() *catalog.Snapshot { return current }, routing.Config{})
			reload := func() {
				t.Helper()
				current, err = catalog.Compile(ctx, f.pool, crypto)
				if err != nil {
					t.Fatal(err)
				}
			}
			assertRouting := func(available bool) {
				t.Helper()
				reload()
				for _, group := range []string{c.RoutingGroup, pool.TokenGroup} {
					req := &gateway.Request{Model: "fixture-model", Principal: gateway.Principal{UserID: 2, Group: group}}
					plan, e := planner.Plan(ctx, req)
					if available {
						if e != nil || len(plan) != 1 || plan[0].ChannelID != c.InternalChannelID || plan[0].Group != c.RoutingGroup {
							t.Fatalf("original group/pool %s not restored: %+v %v", group, plan, e)
						}
					} else if !errors.Is(e, routing.ErrNoRoute) || len(plan) != 0 {
						t.Fatalf("paused channel still routed: %+v %v", plan, e)
					}
				}
				page, e := f.s.ListGroupFavorites(ctx, consumer, 1, 24, nil)
				if e != nil || len(page.Items) != 1 || page.Items[0].GroupID != c.GroupID || page.Items[0].Available != available {
					t.Fatalf("original favorite changed/lost: %+v %v", page, e)
				}
			}
			assertRouting(true)
			if err = f.s.Transition(ctx, owner, c.InternalChannelID, "pause", "maintenance"); err != nil {
				t.Fatal(err)
			}
			assertRouting(false)
			switch via {
			case "owner":
				err = f.s.Transition(ctx, owner, c.InternalChannelID, "resume", "")
			case "catalog", "tag":
				mux := http.NewServeMux()
				catalogcontrol.New(f.pool, crypto, nil).Register(mux, func(h http.Handler) http.Handler { return h })
				path := fmt.Sprintf("/api/catalog/channels/%d", c.InternalChannelID)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				var reply struct {
					Data catalogcontrol.Channel `json:"data"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
					t.Fatalf("read channel: %d %s", w.Code, w.Body)
				}
				reply.Data.Status = "enabled"
				payload, e := json.Marshal(reply.Data)
				if e != nil {
					t.Fatal(e)
				}
				method := "PUT"
				if via == "tag" {
					if _, e = f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET tag='resume-fixture' WHERE id=$1`, c.InternalChannelID); e != nil {
						t.Fatal(e)
					}
					method, path, payload = "POST", "/api/channel/tag/enabled", []byte(`{"tag":"resume-fixture"}`)
				}
				w = httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(payload)))
				if w.Code != 200 {
					t.Fatalf("resume: %d %s", w.Code, w.Body)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			assertRouting(true)
			got, err := f.s.Get(ctx, consumer, c.GroupID)
			if err != nil || got.ID != c.ID || got.RoutingGroup != c.RoutingGroup || got.InternalChannelID != c.InternalChannelID {
				t.Fatalf("resume replaced original identifiers: %+v %v", got, err)
			}
			var bound string
			if err = f.pool.QueryRow(ctx, `SELECT group_name FROM v3_identity.api_keys WHERE id=$1`, key).Scan(&bound); err != nil || bound != c.RoutingGroup {
				t.Fatal("existing API key lost its original group", bound, err)
			}
			if _, err = planner.Plan(ctx, &gateway.Request{Model: "fixture-model", Principal: gateway.Principal{UserID: 3, Group: c.RoutingGroup}}); !errors.Is(err, routing.ErrNoRoute) {
				t.Fatal("resume exposed private group to an uninvited user", err)
			}
		})
	}
}

func TestCatalogEnableCannotBypassMarketReviewAndRollsBackBatch(t *testing.T) {
	for _, state := range []string{"draft", "verifying", "paused", "rejected", "deleted"} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			blocked := f.channel(t, "public")
			verified := f.channel(t, "public")
			f.active(t, verified)
			if err := f.s.Transition(ctx, channelmarket.Actor{UserID: 1}, verified.InternalChannelID, "pause", "maintenance"); err != nil {
				t.Fatal(err)
			}
			verification := "passed"
			if state == "paused" {
				verification = "failed"
			}
			if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status=$2,verification_status=$3,deleted_at=CASE WHEN $2='deleted' THEN now() ELSE NULL END WHERE id=$1`, blocked.GroupID, state, verification); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET tag='guard-fixture' WHERE id=ANY($1)`, []int64{blocked.InternalChannelID, verified.InternalChannelID}); err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			crypto, _ := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
			catalogcontrol.New(f.pool, crypto, nil).Register(mux, func(h http.Handler) http.Handler { return h })
			path := fmt.Sprintf("/api/catalog/channels/%d", blocked.InternalChannelID)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			var reply struct {
				Data catalogcontrol.Channel `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
				t.Fatalf("read channel: %d %s", w.Code, w.Body)
			}
			reply.Data.Status = "enabled"
			payload, err := json.Marshal(reply.Data)
			if err != nil {
				t.Fatal(err)
			}
			for _, request := range []struct {
				method, path string
				payload      []byte
			}{
				{"PUT", path, payload},
				{"POST", "/api/channel/tag/enabled", []byte(`{"tag":"guard-fixture"}`)},
			} {
				w = httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(request.method, request.path, bytes.NewReader(request.payload)))
				if w.Code != http.StatusConflict {
					t.Fatalf("%s bypassed %s review: %d %s", request.path, state, w.Code, w.Body)
				}
			}
			var channelState, marketState string
			if err = f.pool.QueryRow(ctx, `SELECT c.status,g.lifecycle_status FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.id=$1`, blocked.InternalChannelID).Scan(&channelState, &marketState); err != nil || channelState != "disabled" || marketState != state {
				t.Fatal("rejected channel was modified", channelState, marketState, err)
			}
			if err = f.pool.QueryRow(ctx, `SELECT c.status,g.lifecycle_status FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.id=$1`, verified.InternalChannelID).Scan(&channelState, &marketState); err != nil || channelState != "disabled" || marketState != "paused" {
				t.Fatal("failed batch partially resumed another channel", channelState, marketState, err)
			}
		})
	}
}

func TestPauseResumePropagatesThroughOutboxToExistingGateway(t *testing.T) {
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not set")
	}
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	pool, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Cached routes", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}})
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	publisher := catalog.NewPublisher(f.pool, rdb, crypto, crypto, nil)
	if err = publisher.PublishNow(ctx); err != nil {
		t.Fatal(err)
	}
	store := catalog.NewStore(f.pool, rdb, crypto, nil)
	if err = store.Load(ctx); err != nil {
		t.Fatal(err)
	}
	relay := catalog.NewOutboxRelay(f.pool, rdb, publisher, nil)
	runCtx, cancel := context.WithCancel(ctx)
	storeDone, relayDone := make(chan error, 1), make(chan error, 1)
	go func() { storeDone <- store.Run(runCtx) }()
	go func() { relayDone <- relay.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-storeDone; err != nil {
			t.Error(err)
		}
		if err := <-relayDone; err != nil {
			t.Error(err)
		}
	})
	planner := routing.New(store.Current, routing.Config{})
	check := func(available bool) {
		t.Helper()
		for _, group := range []string{c.RoutingGroup, pool.TokenGroup} {
			plan, e := planner.Plan(ctx, &gateway.Request{Model: "fixture-model", Principal: gateway.Principal{UserID: 2, Group: group}})
			if available && (e != nil || len(plan) != 1 || plan[0].ChannelID != c.InternalChannelID) {
				t.Fatalf("existing gateway did not recover original group/pool: %+v %v", plan, e)
			}
			if !available && (!errors.Is(e, routing.ErrNoRoute) || len(plan) != 0) {
				t.Fatalf("existing gateway routed a paused channel: %+v %v", plan, e)
			}
		}
	}
	await := func(previous int64, state string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			snap := store.Current()
			if snap.Version > previous && snap.Market.Groups[c.RoutingGroup].Status == state && (snap.Channels[c.InternalChannelID] != nil) == (state == "active") {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("outbox did not publish %s to existing gateway: version=%d previous=%d", state, store.Version(), previous)
	}
	check(true)
	previous := store.Version()
	if err = f.s.Transition(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, "pause", "maintenance"); err != nil {
		t.Fatal(err)
	}
	await(previous, "paused")
	check(false)
	previous = store.Version()
	mux := http.NewServeMux()
	catalogcontrol.New(f.pool, crypto, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	if _, err = f.pool.Exec(ctx, `UPDATE v3_catalog.channels SET tag='cached-resume' WHERE id=$1`, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/channel/tag/enabled", bytes.NewBufferString(`{"tag":"cached-resume"}`)))
	if w.Code != 200 {
		t.Fatalf("resume %d %s", w.Code, w.Body)
	}
	await(previous, "active")
	check(true)
}
