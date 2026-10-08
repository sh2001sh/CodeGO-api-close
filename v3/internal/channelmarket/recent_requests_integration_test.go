//go:build pgintegration

package channelmarket_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestRecentRequestsPreserveUTCHourEdgesAndRetainedAuditSuppression(t *testing.T) {
	f := setup(t)
	public := f.channel(t, "public")
	private := f.channel(t, "private")
	f.active(t, public)
	f.active(t, private)
	// An offset that is not a whole hour must still yield UTC hour boundaries.
	now := time.Date(2026, 10, 7, 17, 50, 0, 0, time.FixedZone("fixture", 5*3600+1800))
	f.now.Store(now.Unix())
	start := now.UTC().Truncate(time.Hour).Add(-5 * time.Hour)
	audits := audit.New(f.pool, audit.Config{})
	writeAudit := func(id, status string, channel int64, at, completed time.Time, counted bool) {
		t.Helper()
		if err := audits.RecordRequest(ctx, audit.RequestRecord{RequestID: id, Model: "fixture-model", Group: public.RoutingGroup, UserID: 2, KeyID: 1, ChannelID: channel, Status: status, Counted: counted, StartedAt: at, CompletedAt: completed, Attempts: 1, StatusCode: 200}); err != nil {
			t.Fatal(err)
		}
	}
	writeAudit("first-hour-edge", "success", public.InternalChannelID, start, start.Add(time.Second), true)
	writeAudit("second-hour-edge", "failed", public.InternalChannelID, start.Add(time.Hour), start.Add(time.Hour+time.Second), true)
	writeAudit("capture-edge", "success", public.InternalChannelID, now, now, true)
	// Every retained audit suppresses legacy usage, even when that audit cannot
	// participate in this public channel's completed, counted time window.
	writeAudit("suppressed-old", "success", public.InternalChannelID, start.Add(-time.Microsecond), start, true)
	writeAudit("suppressed-future", "success", public.InternalChannelID, now.Add(time.Microsecond), now.Add(time.Second), true)
	writeAudit("suppressed-incomplete", "success", public.InternalChannelID, now.Add(-time.Minute), now.Add(time.Second), true)
	writeAudit("suppressed-uncounted", "cancelled", public.InternalChannelID, now.Add(-time.Minute), now, false)
	writeAudit("suppressed-other-channel", "success", private.InternalChannelID, now.Add(-time.Minute), now, true)
	var wallet, subscription int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'subscription') RETURNING id`).Scan(&subscription); err != nil {
		t.Fatal(err)
	}
	writeUsage := func(id, terminal string, account int64, at time.Time) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES($1,$2,2,$3,0,$4,'fixture-model',$5)`, at, account, public.InternalChannelID, id, terminal); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"suppressed-old", "suppressed-future", "suppressed-incomplete", "suppressed-uncounted", "suppressed-other-channel", "capture-edge"} {
		writeUsage(id, "completed", wallet, now)
	}
	// Split funding across an hour edge remains one request at its first time.
	writeUsage("split-hour-edge", "failed", wallet, start.Add(4*time.Hour-time.Microsecond))
	writeUsage("split-hour-edge", "CompletedNoUsage", subscription, start.Add(4*time.Hour))
	writeUsage("legacy-failure", "failed", wallet, start.Add(4*time.Hour))
	writeUsage("legacy-failure", "failed", subscription, start.Add(4*time.Hour+time.Second))
	writeUsage("legacy-capture-edge", "completed", wallet, now)
	writeUsage("legacy-before-window", "completed", wallet, start.Add(-time.Microsecond))
	writeUsage("legacy-after-capture", "completed", wallet, now.Add(time.Microsecond))
	groups, err := f.s.List(ctx, channelmarket.Actor{}, false)
	if err != nil || len(groups) != 1 || groups[0].GroupID != public.GroupID {
		t.Fatalf("public list includes unauthorized channels: %+v %v", groups, err)
	}
	expected := make([]channelmarket.RecentRequestBucket, 6)
	for i := range expected {
		expected[i].Ts = start.Add(time.Duration(i) * time.Hour).Unix()
	}
	expected[0].RequestCount, expected[0].SuccessRate = 1, 100
	expected[1].RequestCount = 1
	expected[3].RequestCount, expected[3].SuccessRate = 1, 100
	expected[4].RequestCount = 1
	expected[5].RequestCount, expected[5].SuccessRate = 2, 100
	if groups[0].RecentBucketSeconds != 3600 || !reflect.DeepEqual(groups[0].RecentRequests, expected) {
		t.Fatalf("hour edges, split requests, or retained audit suppression changed: got %+v; want %+v", groups[0].RecentRequests, expected)
	}
	detail, err := f.s.Get(ctx, channelmarket.Actor{}, public.GroupID)
	if err != nil || !reflect.DeepEqual(detail.RecentRequests, expected) {
		t.Fatalf("single-channel and full-list statistics differ: %+v %v", detail.RecentRequests, err)
	}
}
