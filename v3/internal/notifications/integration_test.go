//go:build pgintegration

package notifications

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

// This suite only resets its explicitly reserved disposable database. A DSN
// pointing at another database is rejected before any schema mutation.
func TestInboxPersistenceOwnershipReadSyncAndCommitEvents(t *testing.T) {
	dsn := os.Getenv("V3_NOTIFICATIONS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_NOTIFICATIONS_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if database != "notifications_test" {
		t.Fatal("notification integration DSN must target reserved disposable notifications_test")
	}
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
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
			t.Fatalf("migration %s: %v", file, e)
		}
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(1,'notice-owner'),(2,'notice-consumer');
INSERT INTO v3_catalog.channels(id,name,provider,scope,owner_user_id) VALUES(7,'notice-channel','openai','marketplace',1);
INSERT INTO v3_channelmarket.multiplier_notices(id,channel_id,user_id,previous_ppm,multiplier_ppm) VALUES(11,7,2,1000000,1200000);`)
	if err != nil {
		t.Fatal(err)
	}
	s := New(pool, nil)
	v, err := s.List(ctx, 2, Filter{Page: 1, PageSize: 20})
	if err != nil || v.UnreadCount != 1 || v.Total != 1 || len(v.Items) != 1 {
		t.Fatalf("source capture: %+v %v", v, err)
	}
	id := v.Items[0].ID
	if err = s.Read(ctx, 1, id, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign notice was writable: %v", err)
	}
	if err = s.Read(ctx, 2, id, true); err != nil {
		t.Fatal(err)
	}
	if err = s.Read(ctx, 2, id, true); err != nil {
		t.Fatalf("acknowledgement must be idempotent: %v", err)
	}
	if err = s.Read(ctx, 2, id, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Read(ctx, 2, id, false); err != nil {
		t.Fatalf("unread acknowledgement must be idempotent: %v", err)
	}
	state, err := s.Summary(ctx, 2)
	if err != nil || state.UnreadCount != 1 || state.LatestID != id {
		t.Fatalf("restore unread state: %+v %v", state, err)
	}
	if err = s.Read(ctx, 2, id, true); err != nil {
		t.Fatal(err)
	}
	var legacyRead bool
	if err = pool.QueryRow(ctx, `SELECT read_at IS NOT NULL FROM v3_channelmarket.multiplier_notices WHERE id=11`).Scan(&legacyRead); err != nil || !legacyRead {
		t.Fatalf("new->legacy acknowledgement: %v %v", legacyRead, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_channelmarket.multiplier_notices(id,channel_id,user_id,previous_ppm,multiplier_ppm) VALUES(12,7,2,1200000,1100000); UPDATE v3_channelmarket.multiplier_notices SET read_at=now() WHERE id=12`); err != nil {
		t.Fatal(err)
	}
	n, err := s.Summary(ctx, 2)
	if err != nil || n.UnreadCount != 0 {
		t.Fatalf("legacy->new acknowledgement: %+v %v", n, err)
	}

	events, stop, err := s.hub.subscribe(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO v3_channelmarket.multiplier_notices(id,channel_id,user_id,previous_ppm,multiplier_ppm) VALUES(13,7,2,1100000,900000)`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
		t.Fatal("event escaped before source transaction committed")
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	n, err = s.Summary(ctx, 2)
	if err != nil || n.UnreadCount != 0 {
		t.Fatalf("rolled-back notice persisted: %+v %v", n, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_channelmarket.multiplier_notices(id,channel_id,user_id,previous_ppm,multiplier_ppm) VALUES(14,7,2,1100000,900000)`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("committed source change did not emit an event")
	}

	_, err = pool.Exec(ctx, `INSERT INTO v3_commerce.orders(id,user_id,amount_minor,credits,currency,kind,provider,trade_no,expires_at) VALUES(20,2,123,1000000,'usd','topup','test','notice-order',now()+interval '1 day');
UPDATE v3_commerce.orders SET state='paid',paid_at=now() WHERE id=20;
UPDATE v3_commerce.orders SET state='paid' WHERE id=20;
INSERT INTO v3_channelmarket.shops(owner_user_id,review_status,submitted_name) VALUES(1,'pending','Neutral Service');
UPDATE v3_channelmarket.shops SET review_status='approved',name=submitted_name WHERE owner_user_id=1;`)
	if err != nil {
		t.Fatal(err)
	}
	v, err = s.List(ctx, 2, Filter{Category: "billing", Unread: true, Page: 1, PageSize: 20})
	if err != nil || v.Total != 1 || v.UnreadCount != 2 || v.Items[0].Kind != "order_paid" {
		t.Fatalf("billing state dedupe/filter: %+v %v", v, err)
	}
	owner, err := s.List(ctx, 1, Filter{Page: 1, PageSize: 20})
	if err != nil || owner.Total != 1 || owner.Items[0].Kind != "shop_review" {
		t.Fatalf("shop review scoped to owner: %+v %v", owner, err)
	}
	if err = s.ReadAll(ctx, 2, "billing", v.LatestID); err != nil {
		t.Fatal(err)
	}
	n, err = s.Summary(ctx, 2)
	if err != nil || n.UnreadCount != 1 {
		t.Fatalf("category read-all modified other categories: %+v %v", n, err)
	}
	if err = s.ReadAll(ctx, 2, "all", v.LatestID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT read_at IS NOT NULL FROM v3_channelmarket.multiplier_notices WHERE id=14`).Scan(&legacyRead); err != nil || !legacyRead {
		t.Fatalf("read-all failed to mirror source: %v %v", legacyRead, err)
	}
	if _, err = strconv.ParseInt(id, 10, 64); err != nil {
		t.Fatalf("ID is not an exact decimal string: %q", id)
	}
	_, err = pool.Exec(ctx, `INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds) VALUES(90,'notice-plan',100,1000000,3600);
INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,starts_at,expires_at) VALUES(90,2,90,now(),now()+interval '1 hour');
INSERT INTO v3_commerce.subscription_lucky_draws(id,draw_date,winning_number,jackpot_before,jackpot_after,status,timezone,draw_hour,draw_minute,base_reward_1_usd,base_reward_2_usd,base_reward_3_usd,base_reward_4_usd,multiplier_lite,multiplier_standard,multiplier_pro,multiplier_ultra,jackpot_initial_usd,jackpot_increment_usd,jackpot_cap_usd,cost_per_usd,monthly_budget_usd)
VALUES(90,'2026-10-07','1234',0,0,'completed','UTC',0,0,0,0,0,0,1,1,1,1,0,0,0,1,0);
INSERT INTO v3_commerce.subscription_lucky_rewards(id,draw_id,subscription_id,participation_type,user_id,lucky_number,membership_tier,matched_digits,base_reward_usd,tier_multiplier,jackpot_reward_usd,final_reward_credits,credit_status)
VALUES(90,90,90,'subscription',2,'1234','standard',1,0,1,0,9007199254740993,'credited');
INSERT INTO v3_commerce.subscription_lucky_reward_notifications(id,reward_id,user_id) VALUES(90,90,2);`)
	if err != nil {
		t.Fatal(err)
	}
	v, err = s.List(ctx, 2, Filter{Category: "rewards", Page: 1, PageSize: 20})
	if err != nil || len(v.Items) != 1 || v.Items[0].Kind != "lucky_reward" {
		t.Fatalf("lucky source capture: %+v %v", v, err)
	}
	var amounts map[string]string
	if err = json.Unmarshal(v.Items[0].Data, &amounts); err != nil || amounts["final_reward_credits"] != "9007199254740993" {
		t.Fatalf("reward precision was lost: %s %v", v.Items[0].Data, err)
	}
	if err = s.Read(ctx, 2, v.Items[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT read_at IS NOT NULL FROM v3_commerce.subscription_lucky_reward_notifications WHERE id=90`).Scan(&legacyRead); err != nil || !legacyRead {
		t.Fatalf("reward read state was not mirrored: %v %v", legacyRead, err)
	}
	// Observe a global per-user snapshot through a filtered, empty page. A
	// notification arriving afterwards must survive the bulk acknowledgement.
	if err = s.Read(ctx, 2, id, false); err != nil {
		t.Fatal(err)
	}
	observed, err := s.List(ctx, 2, Filter{Category: "billing", Page: 2, PageSize: 1})
	if err != nil || observed.LatestID != v.LatestID || len(observed.Items) != 0 {
		t.Fatalf("latest_id depends on filtered page: %+v %v", observed, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_channelmarket.multiplier_notices(id,channel_id,user_id,previous_ppm,multiplier_ppm) VALUES(15,7,2,900000,800000)`); err != nil {
		t.Fatal(err)
	}
	if err = s.ReadAll(ctx, 2, "all", observed.LatestID); err != nil {
		t.Fatal(err)
	}
	state, err = s.Summary(ctx, 2)
	if err != nil || state.UnreadCount != 1 || state.LatestID == observed.LatestID {
		t.Fatalf("read-all consumed an unseen later notification: %+v %v", state, err)
	}
	if err = s.ReadAll(ctx, 2, "all", state.LatestID); err != nil {
		t.Fatal(err)
	}
	// Channel profile review and a new bargain request go to the owner; the
	// accepted bargain goes to the requesting consumer instead.
	_, err = pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('notice-group');
INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name) VALUES('notice-group','7',7,1,'notice-group','notice-group','Notice Group');
UPDATE v3_catalog.channels SET settings='{"market":{"name_status":"pending"}}' WHERE id=7;
UPDATE v3_catalog.channels SET settings='{"market":{"name_status":"rejected","name_review_reason":"Please revise"}}' WHERE id=7;
INSERT INTO v3_channelmarket.bargain_requests(id,group_id,user_id,proposed_ppm) VALUES('notice-bargain','notice-group',2,500000);
UPDATE v3_channelmarket.bargain_requests SET status='accepted' WHERE id='notice-bargain';`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = s.List(ctx, 1, Filter{Category: "review", Page: 1, PageSize: 20})
	if err != nil || owner.Total != 2 {
		t.Fatalf("shop/channel review category: %+v %v", owner, err)
	}
	owner, err = s.List(ctx, 1, Filter{Category: "market", Page: 1, PageSize: 20})
	if err != nil || owner.Total != 1 || owner.Items[0].Kind != "bargain_requested" {
		t.Fatalf("owner bargain request notice: %+v %v", owner, err)
	}
	v, err = s.List(ctx, 2, Filter{Category: "market", Unread: true, Page: 1, PageSize: 20})
	if err != nil || v.Total != 1 || v.Items[0].Kind != "bargain_resolved" {
		t.Fatalf("consumer bargain outcome: %+v %v", v, err)
	}
	t.Run("SSEWriteDeadlineAndRevokedClientIsolation", func(t *testing.T) {
		var revoked atomic.Bool
		mux := http.NewServeMux()
		s.Register(mux, func(r *http.Request) (int64, error) {
			c, e := r.Cookie("codego_session")
			if e != nil {
				return 0, e
			}
			if c.Value == "owner-session" {
				return 1, nil
			}
			if c.Value == "consumer-session" && !revoked.Load() {
				return 2, nil
			}
			return 0, errors.New("revoked session")
		})
		server := httptest.NewUnstartedServer(mux)
		server.Config.WriteTimeout = 40 * time.Millisecond
		server.Start()
		defer server.Close()
		streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		open := func(cookie string) (*http.Response, *bufio.Scanner) {
			r, e := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/api/notifications/events", nil)
			if e != nil {
				t.Fatal(e)
			}
			r.AddCookie(&http.Cookie{Name: "codego_session", Value: cookie})
			res, e := server.Client().Do(r)
			if e != nil {
				t.Fatal(e)
			}
			if res.StatusCode != 200 {
				_ = res.Body.Close()
				t.Fatalf("stream status %d", res.StatusCode)
			}
			return res, bufio.NewScanner(res.Body)
		}
		readSummary := func(scanner *bufio.Scanner) Summary {
			for scanner.Scan() {
				if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
					var summary Summary
					if e := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &summary); e != nil {
						t.Fatal(e)
					}
					return summary
				}
			}
			t.Fatalf("stream ended before event: %v", scanner.Err())
			return Summary{}
		}
		consumer, consumerEvents := open("consumer-session")
		defer func() { _ = consumer.Body.Close() }()
		owner, ownerEvents := open("owner-session")
		defer func() { _ = owner.Body.Close() }()
		_ = readSummary(consumerEvents)
		_ = readSummary(ownerEvents)
		time.Sleep(80 * time.Millisecond) // beyond the server's ordinary write timeout
		var latest string
		if e := pool.QueryRow(ctx, `INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url) VALUES(2,'sse-test-consumer','system','test','test','test','{}','/notifications') RETURNING id::text`).Scan(&latest); e != nil {
			t.Fatal(e)
		}
		for got := readSummary(consumerEvents); got.LatestID != latest; got = readSummary(consumerEvents) {
		}
		revoked.Store(true)
		if _, e := pool.Exec(ctx, `INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url) VALUES(2,'sse-test-revoked','system','test','test','test','{}','/notifications')`); e != nil {
			t.Fatal(e)
		}
		if e := pool.QueryRow(ctx, `INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url) VALUES(1,'sse-test-owner','system','test','test','test','{}','/notifications') RETURNING id::text`).Scan(&latest); e != nil {
			t.Fatal(e)
		}
		for got := readSummary(ownerEvents); got.LatestID != latest; got = readSummary(ownerEvents) {
		}
		for consumerEvents.Scan() {
			if strings.HasPrefix(consumerEvents.Text(), "data: ") {
				t.Fatal("revoked session received another private notification")
			}
		}
		if consumerEvents.Err() != nil {
			t.Fatalf("revoked stream should close cleanly: %v", consumerEvents.Err())
		}
	})
}
