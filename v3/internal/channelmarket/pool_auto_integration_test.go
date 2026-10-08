//go:build pgintegration

package channelmarket_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestPoolAutoBuildFiltersEligibilityAndUsesEffectiveMultiplier(t *testing.T) {
	f := setup(t)
	good := f.channel(t, "public")
	f.active(t, good)
	over := f.channel(t, "public")
	f.active(t, over)
	private := f.channel(t, "private")
	f.active(t, private)
	paused := f.channel(t, "public")
	f.active(t, paused)
	blocked := f.channel(t, "public")
	f.active(t, blocked)
	noKey := f.channel(t, "public")
	f.active(t, noKey)
	noModels := f.channel(t, "public")
	f.active(t, noModels)
	discount := f.channel(t, "public")
	f.active(t, discount)
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET multiplier_ppm=2000000 WHERE id=ANY($1)`, []string{over.GroupID, discount.GroupID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.user_multipliers(channel_id,user_id,multiplier_ppm) VALUES($1,2,200000)`, discount.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Transition(ctx, channelmarket.Actor{UserID: 1}, paused.InternalChannelID, "pause", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, blocked.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE v3_catalog.channel_credentials SET status='disabled' WHERE channel_id=$1`, noKey.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM v3_catalog.channel_models WHERE channel_id=$1`, noModels.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	// Only fresh measured usage wins; an old high score is ignored.
	for _, sample := range []struct {
		c     channelmarket.ChannelView
		stamp time.Time
	}{
		{good, time.Unix(f.now.Load(), 0).Add(-2 * time.Hour)},
		{discount, time.Unix(f.now.Load(), 0)},
	} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.ranking_snapshots(id,group_id,window_hours,ranking_version,rank,score,wilson_success_rate,request_count,observing,calculated_at) VALUES($1,$2,24,'v3-usage',1,99,0.99,50,false,$3)`, "fixture:"+sample.c.GroupID, sample.c.GroupID, sample.stamp); err != nil {
			t.Fatal(err)
		}
	}
	p, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Configured", Strategy: "priority", MaxMultiplier: json.Number("0.5"), Members: []channelmarket.PoolMember{{GroupID: good.GroupID}}, AutoBuild: json.RawMessage(`{"enabled":true,"models":["fixture-model"],"size":3,"interval_minutes":30,"consumer_weight":0,"success_weight":100,"cache_weight":0}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.BuildPool(ctx, 1, p.ID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("foreign build: %v", err)
	}
	built, err := f.s.BuildPool(ctx, 2, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Members) != 2 || built.Members[0].GroupID != discount.GroupID || built.Members[1].GroupID != good.GroupID {
		t.Fatalf("ineligible members or stale scoring: %+v", built.Members)
	}
	var projected int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM v3_catalog.route_pool_members m JOIN v3_catalog.route_pools p ON p.id=m.pool_id WHERE p.group_name=$1`, built.TokenGroup).Scan(&projected); err != nil || projected != 2 {
		t.Fatalf("projection %d %v", projected, err)
	}
	// Editing the display name cannot forge worker timestamps or errors.
	built.Name = "Renamed"
	built.AutoBuild = json.RawMessage(`{"enabled":true,"models":["fixture-model"],"size":3,"interval_minutes":30,"consumer_weight":0,"success_weight":100,"cache_weight":0,"last_build_at":"2030-01-01T00:00:00Z","next_build_at":"2030-01-01T00:00:00Z","last_error":"forged"}`)
	edited, err := f.s.SavePool(ctx, 2, built)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Last *time.Time `json:"last_build_at"`
		Next *time.Time `json:"next_build_at"`
		Err  string     `json:"last_error"`
	}
	if err = json.Unmarshal(edited.AutoBuild, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Last == nil || metadata.Last.Unix() != f.now.Load() || metadata.Next == nil || metadata.Next.Unix() != f.now.Load()+30*60 || metadata.Err != "" || edited.ID != p.ID || edited.TokenGroup != p.TokenGroup {
		t.Fatalf("forged metadata or changed routing identity %+v", metadata)
	}
}

func TestPoolEmptyBuildRetainsMembersAndDoesNotStarveOtherPools(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	failing, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Missing model", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}, AutoBuild: json.RawMessage(`{"enabled":true,"models":["missing-model"],"size":1,"interval_minutes":60}`)})
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Working model", AutoBuild: json.RawMessage(`{"enabled":true,"models":["fixture-model"],"size":1,"interval_minutes":60}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Schedule the failure first to prove the scheduler continues after it.
	if _, err = f.pool.Exec(ctx, `UPDATE v3_channelmarket.route_pools SET config=jsonb_set(config,'{auto_build,next_build_at}',to_jsonb($2::timestamptz)) WHERE id=$1`, failing.ID, time.Unix(f.now.Load(), 0).Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	var key int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_identity.api_keys(user_id,key_hash,key_prefix,key_ciphertext,group_name) VALUES(2,decode(repeat('ab',32),'hex'),'fixture','fixture',$1) RETURNING id`, failing.TokenGroup).Scan(&key); err != nil {
		t.Fatal(err)
	}
	count, err := f.s.RebuildPools(ctx, 100)
	if count != 1 || !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("scheduler stopped at first failure: count=%d %v", count, err)
	}
	pools, err := f.s.Pools(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pools {
		if len(p.Members) != 1 || p.Members[0].GroupID != c.GroupID {
			t.Fatalf("members erased or other pool not built %+v", p)
		}
		var metadata struct {
			Last *time.Time `json:"last_build_at"`
			Next *time.Time `json:"next_build_at"`
			Err  string     `json:"last_error"`
		}
		if err = json.Unmarshal(p.AutoBuild, &metadata); err != nil {
			t.Fatal(err)
		}
		if p.ID == failing.ID && (metadata.Last != nil || metadata.Next == nil || metadata.Next.Unix() != f.now.Load()+15*60 || metadata.Err == "") {
			t.Fatalf("failure metadata %+v", metadata)
		}
		if p.ID == healthy.ID && (metadata.Last == nil || metadata.Err != "") {
			t.Fatalf("other pool not built %+v", metadata)
		}
	}
	var group string
	if err = f.pool.QueryRow(ctx, `SELECT group_name FROM v3_identity.api_keys WHERE id=$1`, key).Scan(&group); err != nil || group != failing.TokenGroup {
		t.Fatalf("key binding changed %s %v", group, err)
	}
	if count, err = f.s.RebuildPools(ctx, 100); err != nil || count != 0 {
		t.Fatalf("future scheduled pools rebuilt immediately %d %v", count, err)
	}
}

func TestPoolConcurrentWorkersBuildDuePoolOnce(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	if _, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Once", AutoBuild: json.RawMessage(`{"enabled":true,"models":["fixture-model"],"size":1,"interval_minutes":60}`)}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, err := f.s.RebuildPools(ctx, 100)
			counts <- count
			failures <- err
		}()
	}
	wg.Wait()
	if total := <-counts + <-counts; total != 1 {
		t.Fatalf("pool built %d times", total)
	}
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
}

func TestPoolAutomaticBuildIncludesAuthorizedOfficialGroups(t *testing.T) {
	f := setup(t)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default');
	INSERT INTO v3_catalog.channels(id,name,provider,scope) VALUES(900,'Official','openai','official');
	INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(900,'default');
	INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(900,'official-model');
	INSERT INTO v3_catalog.channel_credentials(channel_id,secret) VALUES(900,'fixture')`); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Official", MaxMultiplier: json.Number("1"), AutoBuild: json.RawMessage(`{"enabled":false,"models":["official-model"],"size":1,"interval_minutes":60}`)})
	if err != nil {
		t.Fatal(err)
	}
	p, err = f.s.BuildPool(ctx, 2, p.ID)
	if err != nil || len(p.Members) != 1 || p.Members[0].GroupID != "official:default" {
		t.Fatalf("authorized official omitted %+v %v", p.Members, err)
	}
	var metadata struct {
		Next *time.Time `json:"next_build_at"`
	}
	if err = json.Unmarshal(p.AutoBuild, &metadata); err != nil || metadata.Next != nil {
		t.Fatalf("disabled plan unexpectedly scheduled %+v %v", metadata, err)
	}
}

func TestPoolDeletedKeyCannotBeBoundOrPreventRemoval(t *testing.T) {
	f := setup(t)
	p, err := f.s.SavePool(ctx, 2, channelmarket.RoutePool{Name: "Disposable"})
	if err != nil {
		t.Fatal(err)
	}
	var key int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_identity.api_keys(user_id,key_hash,key_prefix,key_ciphertext,group_name) VALUES(2,decode(repeat('cd',32),'hex'),'fixture','fixture',$1) RETURNING id`, p.TokenGroup).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DeletePool(ctx, 2, p.ID); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("active key pool removed: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_identity.api_keys SET deleted_at=now() WHERE id=$1`, key); err != nil {
		t.Fatal(err)
	}
	if err = f.s.BindPoolToken(ctx, 2, p.ID, key); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("deleted key rebound: %v", err)
	}
	if err = f.s.DeletePool(ctx, 2, p.ID); err != nil {
		t.Fatalf("deleted key permanently prevents pool removal: %v", err)
	}
}
