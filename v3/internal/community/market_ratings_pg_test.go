//go:build pgintegration

package community

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func recordRealUsage(t *testing.T, pool *pgxpool.Pool, user, channel int64, kind string, counted bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO v3_audit.request_audits(request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at)
 VALUES($1,'trace',$2,0,'model','group','openai_chat',$3,'failed',$4,false,0,0,0,$5,1,0,502,'upstream_error',now(),now(),now(),now())`, fmt.Sprintf("use-%d-%d-%s-%t", user, channel, kind, counted), user, kind, counted, channel)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMarketRatingAuthorityAndPublicSummary(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,external_id) VALUES(1,'owner',NULL),(2,'viewer','DEF567'),(3,'other','GHJ678');
 INSERT INTO v3_catalog.groups(name) VALUES('market_test');
 INSERT INTO v3_catalog.channels(id,name,provider,scope,owner_user_id,settings) VALUES(11,'group','openai','marketplace',1,'{"community":{"id":"12345","visibility":"public","verification_status":"passed","lifecycle_status":"active"}}');
 INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,visibility,lifecycle_status,verification_status) VALUES('internal-group','12345',11,1,'slug','market_test','Approved group','public','active','passed');`)
	if err != nil {
		t.Fatal(err)
	}
	s := New(pool, Config{})
	r, err := s.GetMarketRating(ctx, "12345", 0)
	if err != nil || r.GroupID != "internal-group" || r.CanRate || r.EligibilityReason != "login_required" {
		t.Fatalf("public summary %+v %v", r, err)
	}
	if _, err = s.RateMarketGroup(ctx, "12345", 2, "DEF567", 5); !errors.Is(err, ErrUsageRequired) {
		t.Fatalf("unused voter: %v", err)
	}
	recordRealUsage(t, pool, 2, 11, "probe", true)
	recordRealUsage(t, pool, 2, 11, "sync", false)
	if _, err = s.RateMarketGroup(ctx, "12345", 2, "DEF567", 5); !errors.Is(err, ErrUsageRequired) {
		t.Fatalf("probe/policy: %v", err)
	}
	recordRealUsage(t, pool, 2, 11, "stream", true) // A real failed call is valid feedback.
	if _, err = s.RateMarketGroup(ctx, "12345", 2, "GHJ678", 5); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("forged subject: %v", err)
	}
	r, err = s.RateMarketGroup(ctx, "internal-group", 2, "DEF567", 4)
	if err != nil || !r.CanRate || r.Channel.AverageScore != 8 || r.Seller.RatingCount != 1 {
		t.Fatalf("rating %+v %v", r, err)
	}
	var ownerSub string
	var before, after int
	if err = pool.QueryRow(ctx, `SELECT external_id FROM v3_identity.users WHERE id=1`).Scan(&ownerSub); err != nil || len(ownerSub) != 6 {
		t.Fatalf("new owner bridge %q %v", ownerSub, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_community.rating_event_outbox`).Scan(&before); err != nil || before != 1 {
		t.Fatalf("event count %d %v", before, err)
	}
	if _, err = s.RateMarketGroup(ctx, "12345", 2, "DEF567", 4); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v3_community.rating_event_outbox`).Scan(&after); err != nil || before != after {
		t.Fatalf("same-score event %d %d %v", before, after, err)
	}
	if _, err = s.RateMarketGroup(ctx, "12345", 1, ownerSub, 5); !errors.Is(err, ErrSelfRating) {
		t.Fatalf("self rating %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES(11,2)`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RateMarketGroup(ctx, "12345", 2, "DEF567", 1); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("blocked rating %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET visibility='private' WHERE id='internal-group'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetMarketRating(ctx, "12345", 0); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("private summary %v", err)
	}
}
