//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestNameRemarkJointReviewPreservesPendingPartialEditsAndClear(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	owner, admin := channelmarket.Actor{UserID: 1}, channelmarket.Actor{UserID: 3, Admin: true}
	staged, err := f.s.Update(ctx, owner, c.InternalChannelID, json.RawMessage(`{"name":"多模态研究","remark":"支持流式输出与工具调用。"}`))
	if err != nil || staged.Name != c.Name || staged.Remark != "" || staged.SubmittedRemark == nil || *staged.SubmittedRemark != "支持流式输出与工具调用。" || staged.NameStatus != "pending" || staged.Status != "active" {
		t.Fatalf("joint public edit not staged: %+v %v", staged, err)
	}
	staged, err = f.s.Update(ctx, owner, c.InternalChannelID, json.RawMessage(`{"remark":"支持流式输出、图像与工具调用。"}`))
	if err != nil || staged.SubmittedName != "多模态研究" || staged.SubmittedRemark == nil || *staged.SubmittedRemark != "支持流式输出、图像与工具调用。" {
		t.Fatalf("partial remark patch lost pending name: %+v %v", staged, err)
	}
	public, err := f.s.Get(ctx, channelmarket.Actor{}, c.ID)
	if err != nil || public.Name != c.Name || public.Remark != "" || public.SubmittedName != "" || public.SubmittedRemark != nil || public.NameStatus != "" {
		t.Fatalf("pending presentation leaked: %+v %v", public, err)
	}
	if err = f.s.Transition(ctx, admin, c.InternalChannelID, "reject", "请使用服务说明"); err != nil {
		t.Fatal(err)
	}
	rejected, err := f.s.Get(ctx, owner, c.GroupID)
	if err != nil || rejected.Status != "active" || rejected.Name != c.Name || rejected.Remark != "" || rejected.NameStatus != "rejected" {
		t.Fatalf("presentation rejection disabled/changed service: %+v %v", rejected, err)
	}
	if _, err = f.s.Update(ctx, owner, c.InternalChannelID, json.RawMessage(`{"name":"多模态研究","remark":"支持流式输出、图像与工具调用。"}`)); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Transition(ctx, admin, c.InternalChannelID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	approved, err := f.s.Get(ctx, channelmarket.Actor{}, c.GroupID)
	if err != nil || approved.Name != "多模态研究" || approved.Remark != "支持流式输出、图像与工具调用。" || approved.Status != "active" || approved.ID != c.ID || approved.RoutingGroup != c.RoutingGroup {
		t.Fatalf("approval changed routing or lost remark: %+v %v", approved, err)
	}
	clearing, err := f.s.Update(ctx, owner, c.InternalChannelID, json.RawMessage(`{"remark":""}`))
	if err != nil || clearing.SubmittedRemark == nil || *clearing.SubmittedRemark != "" || clearing.Remark != approved.Remark || clearing.NameStatus != "pending" {
		t.Fatalf("empty candidate did not retain old approved remark: %+v %v", clearing, err)
	}
	encoded, _ := json.Marshal(clearing)
	if !bytes.Contains(encoded, []byte(`"submitted_remark":""`)) {
		t.Fatalf("empty candidate cannot be distinguished from missing: %s", encoded)
	}
	if err = f.s.Transition(ctx, admin, c.InternalChannelID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := f.s.Get(ctx, channelmarket.Actor{}, c.GroupID)
	if err != nil || cleared.Remark != "" || cleared.Name != approved.Name || cleared.Status != "active" {
		t.Fatalf("remark clear failed: %+v %v", cleared, err)
	}
	if _, err = f.s.Update(ctx, owner, c.InternalChannelID, json.RawMessage(`{"remark":"联系客服 example.com"}`)); !errors.Is(err, channelmarket.ErrInvalidRemark) {
		t.Fatalf("bad public remark accepted: %v", err)
	}
}

func TestPublicNumericIDsDoNotCollideWithImportedIDsOrRewriteAliases(t *testing.T) {
	f := setup(t)
	legacy := f.channel(t, "public")
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET public_channel_id='9000' WHERE id=$1`, legacy.GroupID); err != nil {
		t.Fatal(err)
	}
	nonnumeric := f.channel(t, "public")
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET public_channel_id='legacy-fixture-id' WHERE id=$1`, nonnumeric.GroupID); err != nil {
		t.Fatal(err)
	}
	deleted := f.channel(t, "public")
	if _, err := f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET public_channel_id='9009',lifecycle_status='deleted',deleted_at=now() WHERE id=$1`, deleted.GroupID); err != nil {
		t.Fatal(err)
	}
	created := f.channel(t, "public")
	if created.ID != "9010" || created.ID == strconv.FormatInt(created.InternalChannelID, 10) {
		t.Fatalf("new public ID reused catalog/legacy ID: %+v", created)
	}
	for _, item := range []struct{ group, id string }{{legacy.GroupID, "9000"}, {nonnumeric.GroupID, "legacy-fixture-id"}} {
		current, err := f.s.Get(ctx, channelmarket.Actor{UserID: 1}, item.group)
		if err != nil || current.ID != item.id {
			t.Fatalf("legacy public identity rewritten: %+v %v", current, err)
		}
	}
	var communityID string
	if err := f.pool.QueryRow(ctx, `SELECT settings->'community'->>'id' FROM v3_catalog.channels WHERE id=$1`, created.InternalChannelID).Scan(&communityID); err != nil || communityID != created.ID {
		t.Fatalf("community uses catalog ID instead of public ID: %q %v", communityID, err)
	}
	type result struct {
		channel channelmarket.ChannelView
		err     error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			channel, err := f.s.Create(ctx, 1, channelmarket.CreateRequest{Provider: "openai_compatible", BaseURL: "https://example.com", APIKey: "fixture", Models: []string{"fixture-model"}, Visibility: "public"})
			results <- result{channel, err}
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{created.ID: true}
	for result := range results {
		value, err := strconv.ParseInt(result.channel.ID, 10, 64)
		if result.err != nil || err != nil || value <= 9010 || seen[result.channel.ID] {
			t.Fatalf("concurrent public allocation collided: %+v %v", result, err)
		}
		seen[result.channel.ID] = true
	}
}

func TestRecentRequestsUseActualAuditsDedupBillingAndRestrictAccess(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	private := f.channel(t, "private")
	f.active(t, public)
	f.active(t, private)
	now := time.Unix(f.now.Load(), 0)
	audits := audit.New(f.pool, audit.Config{})
	write := func(id, status string, channel int64, at time.Time, counted bool) {
		t.Helper()
		err := audits.RecordRequest(ctx, audit.RequestRecord{RequestID: id, Model: "fixture-model", Group: public.RoutingGroup, UserID: 2, KeyID: 1, ChannelID: channel, Status: status, Counted: counted, StartedAt: at, CompletedAt: at.Add(time.Millisecond), Attempts: 1, StatusCode: 200})
		if err != nil {
			t.Fatal(err)
		}
	}
	write("audit-success", "success", public.InternalChannelID, now.Add(-10*time.Minute), true)
	write("audit-failed", "failed", public.InternalChannelID, now.Add(-10*time.Minute), true)
	write("audit-excluded", "cancelled", public.InternalChannelID, now.Add(-10*time.Minute), false)
	write("private-request-secret", "failed", private.InternalChannelID, now.Add(-10*time.Minute), true)
	write("old-request", "success", public.InternalChannelID, now.Add(-5*time.Hour-time.Second), true)
	write("future-request", "success", public.InternalChannelID, now.Add(time.Second), true)
	// The first slot is inclusive; complete before the capture time.
	write("window-edge", "success", public.InternalChannelID, now.Add(-5*time.Hour), true)
	var wallet, subscription int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'subscription') RETURNING id`).Scan(&subscription); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES
	($1,$2,2,$4,20,'audit-success','fixture-model','completed'),($1,$3,2,$4,30,'audit-success','fixture-model','completed'),
	($1,$2,2,$4,20,'billing-success','fixture-model','completed'),($1,$3,2,$4,30,'billing-success','fixture-model','completed'),
	($1,$2,2,$4,0,'billing-failed','fixture-model','failed'),($1,$2,2,$4,0,'audit-excluded','fixture-model','completed')`, now.Add(-10*time.Minute), wallet, subscription, public.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	groups, err := f.s.List(ctx, channelmarket.Actor{}, false)
	if err != nil || len(groups) != 1 || groups[0].GroupID != public.GroupID || groups[0].RecentBucketSeconds != 3600 || len(groups[0].RecentRequests) != 6 {
		t.Fatalf("unauthorized recent channels exposed: %+v %v", groups, err)
	}
	series := groups[0].RecentRequests
	if series[0].RequestCount != 1 || series[0].SuccessRate != 100 || series[4].RequestCount != 4 || series[4].SuccessRate != 50 || series[5].RequestCount != 0 {
		t.Fatalf("failed/unbilled/duplicate/out-of-window results wrong: %+v", series)
	}
	for i := 1; i < 4; i++ {
		if series[i].RequestCount != 0 {
			t.Fatalf("probe/empty slot looks like traffic: %+v", series[i])
		}
	}
	if _, err = f.s.Get(ctx, channelmarket.Actor{}, private.ID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("private recent data exposed: %v", err)
	}
	owner, err := f.s.Get(ctx, channelmarket.Actor{UserID: 1}, private.GroupID)
	if err != nil || owner.RecentRequests[4].RequestCount != 1 || owner.RecentRequests[4].SuccessRate != 0 {
		t.Fatalf("authorized private failure missing: %+v %v", owner, err)
	}
	encoded, _ := json.Marshal(groups[0].RecentRequests)
	if bytes.Contains(encoded, []byte("audit")) || bytes.Contains(encoded, []byte("request_id")) || bytes.Contains(encoded, []byte("user_id")) {
		t.Fatalf("series leaks metadata: %s", encoded)
	}
	if _, err = f.s.RefreshRankings(ctx); err != nil {
		t.Fatal(err)
	}
	quality, err := f.s.Get(ctx, channelmarket.Actor{}, public.GroupID)
	// 24h adds the old-request outside the six-slot window, but excludes future/cancelled audits.
	if err != nil || quality.Quality == nil || quality.Quality.RequestCount != 6 || quality.Quality.SuccessRate == nil || *quality.Quality.SuccessRate != 4.0/6.0 {
		t.Fatalf("ranking omitted uncharged failures or duplicated audit/billing: %+v %v", quality.Quality, err)
	}
	if err = f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, public.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	blocked, err := f.s.List(ctx, channelmarket.Actor{UserID: 2}, false)
	if err != nil || len(blocked) != 0 {
		t.Fatalf("blocked user receives recent series: %+v %v", blocked, err)
	}
}
