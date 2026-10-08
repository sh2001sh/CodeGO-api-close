//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestPublishedNameReviewPreservesRoutingBindingsAndAccess(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	actor := channelmarket.Actor{UserID: 1}
	admin := channelmarket.Actor{UserID: 3, Admin: true}
	var key int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_identity.api_keys(user_id,key_hash,key_prefix,key_ciphertext) VALUES(2,$1,'fixture','fixture') RETURNING id`, bytes.Repeat([]byte{1}, 32)).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := f.s.BindToken(ctx, 2, c.GroupID, key); err != nil {
		t.Fatal(err)
	}
	pool, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Fixture route", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.s.Update(ctx, actor, c.InternalChannelID, json.RawMessage(`{"name":"通用推理","tags":["openai","google","openai"]}`))
	if err != nil || updated.Name != c.Name || updated.SubmittedName != "通用推理" || updated.NameStatus != "pending" || updated.Status != "active" || len(updated.Tags) != 2 {
		t.Fatalf("published rename not staged safely: %+v %v", updated, err)
	}
	for _, identity := range []string{c.ID, c.PublicSlug, c.GroupID} {
		public, err := f.s.Get(ctx, channelmarket.Actor{}, identity)
		if err != nil || public.Name != c.Name || public.SubmittedName != "" || public.NameStatus != "" {
			t.Fatalf("public pending name leak: %+v %v", public, err)
		}
	}
	if _, err := f.s.Update(ctx, channelmarket.Actor{UserID: 2}, c.InternalChannelID, json.RawMessage(`{"name":"Foreign model"}`)); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("cross-owner rename accepted: %v", err)
	}
	if err := f.s.Transition(ctx, admin, c.InternalChannelID, "reject", "描述不适合作为分组名称"); err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.Get(ctx, actor, c.GroupID)
	if err != nil || owner.Name != c.Name || owner.Status != "active" || owner.NameStatus != "rejected" || owner.NameReviewReason == "" {
		t.Fatalf("name rejection stopped service: %+v %v", owner, err)
	}
	if _, err := f.s.Update(ctx, actor, c.InternalChannelID, json.RawMessage(`{"name":"多模态研究"}`)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Transition(ctx, admin, c.InternalChannelID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	approved, err := f.s.Get(ctx, channelmarket.Actor{}, c.GroupID)
	if err != nil || approved.Name != "多模态研究" || approved.ID != c.ID || approved.GroupID != c.GroupID || approved.PublicSlug != c.PublicSlug || approved.RoutingGroup != c.RoutingGroup || approved.Verification != "passed" {
		t.Fatalf("approval changed identity or verification: %+v %v", approved, err)
	}
	var routing, communityName string
	if err := f.pool.QueryRow(ctx, `SELECT k.group_name,c.settings->'community'->>'name' FROM v3_identity.api_keys k JOIN v3_catalog.channels c ON c.id=$2 WHERE k.id=$1`, key, c.InternalChannelID).Scan(&routing, &communityName); err != nil || routing != c.RoutingGroup || communityName != approved.Name {
		t.Fatalf("bound key/community identity changed %q %q %v", routing, communityName, err)
	}
	if routes, err := f.s.Pools(ctx, 2); err != nil || len(routes) != 1 || routes[0].ID != pool.ID || routes[0].Members[0].GroupID != c.GroupID {
		t.Fatalf("route member changed %+v %v", routes, err)
	}
	if _, err := f.s.Update(ctx, actor, c.InternalChannelID, json.RawMessage(`{"tags":[]}`)); err != nil {
		t.Fatal(err)
	}
	cleared, err := f.s.Get(ctx, actor, c.GroupID)
	if err != nil || len(cleared.Tags) != 0 || cleared.Status != "active" || cleared.Verification != "passed" {
		t.Fatalf("tags not clearable or disabled service: %+v %v", cleared, err)
	}
}

func TestQualitySnapshotsDeduplicateSplitBillingAndStayAbsentWithoutSamples(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	empty := f.channel(t, "public")
	f.active(t, c)
	f.active(t, empty)
	before, err := f.s.Get(ctx, channelmarket.Actor{}, c.GroupID)
	if err != nil || before.Quality != nil {
		t.Fatalf("unmeasured snapshot invented: %+v %v", before.Quality, err)
	}
	var wallet, subscription int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'subscription') RETURNING id`).Scan(&subscription); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(f.now.Load(), 0)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,prompt_tokens,cached_tokens,request_id,model,terminal) VALUES
	($1,$2,2,$4,200000,100,25,'split-request','fixture-model','completed'),
	($1,$3,2,$4,300000,100,25,'split-request','fixture-model','completed'),
	($1,$2,2,$4,0,0,0,'failed-request','fixture-model','failed'),
	($1-interval '25 hours',$2,2,$4,100,100,0,'old-request','fixture-model','completed')`, now, wallet, subscription, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if count, err := f.s.RefreshRankings(ctx); err != nil || count != 2 {
		t.Fatalf("refresh %d %v", count, err)
	}
	measured, err := f.s.Get(ctx, channelmarket.Actor{}, c.GroupID)
	if err != nil || measured.Quality == nil {
		t.Fatalf("missing persisted quality %+v %v", measured, err)
	}
	q := measured.Quality
	if q.RequestCount != 2 || q.SuccessRate == nil || *q.SuccessRate != 0.5 || q.CacheHitRate == nil || *q.CacheHitRate != 0.25 || q.AverageChargeCredits == nil || *q.AverageChargeCredits != "0.25" || !q.Observing || !q.CalculatedAt.Equal(now) {
		t.Fatalf("split billing counted as independent requests: %+v", q)
	}
	noUsage, err := f.s.Get(ctx, channelmarket.Actor{}, empty.GroupID)
	if err != nil || noUsage.Quality == nil || noUsage.Quality.SuccessRate != nil || noUsage.Quality.CacheHitRate != nil || noUsage.Quality.AverageChargeCredits != nil || noUsage.Quality.RequestCount != 0 {
		t.Fatalf("empty samples advertised measurements: %+v %v", noUsage.Quality, err)
	}
}

func TestPrivateEffectiveQuotesAndRejectedInputs(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "private")
	f.active(t, c)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_catalog.model_prices(model,mode,input_per_mtok,output_per_mtok) VALUES('fixture-model','per_token',2000000,6000000)`); err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.Get(ctx, channelmarket.Actor{UserID: 1}, c.GroupID)
	if err != nil || owner.EffectivePrices["fixture-model"].InputPerMillion != "0.2" || owner.EffectivePrices["fixture-model"].OutputPerMillion != "0.6" {
		t.Fatalf("private pricing lacks final multiplier: %+v %v", owner.EffectivePrices, err)
	}
	if _, err := f.s.Get(ctx, channelmarket.Actor{}, c.GroupID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("private quote leaked: %v", err)
	}
	for _, patch := range []json.RawMessage{json.RawMessage(`{"name":"微信abc"}`), json.RawMessage(`{"name":"example.com"}`), json.RawMessage(`{"tags":["external-link"]}`), json.RawMessage(`{"group_id":"changed"}`)} {
		if _, err := f.s.Update(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, patch); !errors.Is(err, channelmarket.ErrInvalid) {
			t.Fatalf("accepted prohibited patch %s: %v", patch, err)
		}
	}
	unchanged, err := f.s.Get(ctx, channelmarket.Actor{UserID: 1}, c.GroupID)
	if err != nil || unchanged.ID != c.ID || unchanged.Name != c.Name || unchanged.NameStatus != "approved" {
		t.Fatalf("rejected update persisted state: %+v %v", unchanged, err)
	}
}
