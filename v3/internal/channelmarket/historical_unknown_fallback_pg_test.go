//go:build pgintegration

package channelmarket_test

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestHistoricalUnknownAuditsDoNotSuppressUsageFallback(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	stamp := time.Unix(f.now.Load(), 0).Add(-time.Hour)
	var account int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_audit.request_audits(request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at)
	 VALUES('unknown-http','',2,1,'fixture-model','','openai','stream','historical_unknown',false,false,0,0,0,$1,0,0,0,'',$2,'0001-01-01T00:00:00Z',$2,$2),
	 ('excluded-http','',2,1,'fixture-model','','openai','stream','cancelled',false,false,0,0,0,$1,0,0,499,'',$2,$2,$2,$2)`, c.InternalChannelID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES
	 ($2,$3,2,$1,10,'unknown-http','fixture-model','completed'),($2+interval '1 second',$3,2,$1,5,'unknown-http','fixture-model','completed'),($2,$3,2,$1,9,'excluded-http','fixture-model','completed')`, c.InternalChannelID, stamp, account); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.ModelInsights(ctx, channelmarket.Actor{}, c.ID, "fixture-model", 24)
	if err != nil || got.RequestCount != 1 || got.SuccessCount != 1 || got.PerformanceSamples != 0 {
		t.Fatalf("usage hidden or HTTP measurement invented: %+v %v", got, err)
	}
	groups, err := f.s.List(ctx, channelmarket.Actor{}, false)
	if err != nil || len(groups) != 1 {
		t.Fatalf("list=%+v err=%v", groups, err)
	}
	var count int64
	for _, bucket := range groups[0].RecentRequests {
		count += bucket.RequestCount
	}
	if count != 1 {
		t.Fatalf("recent usage hidden or duplicated count=%d", count)
	}
	if _, err = f.s.RefreshRankings(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, channelmarket.OwnerAnalyticsFilter{From: stamp.Add(-time.Hour), To: stamp.Add(2 * time.Hour)})
	if err != nil || owner.Summary.RequestCount != 1 || owner.Summary.SuccessCount != 1 {
		t.Fatalf("owner usage hidden or duplicated %+v %v", owner, err)
	}
	var status string
	if err = f.pool.QueryRow(ctx, `SELECT status FROM v3_audit.request_audits WHERE request_id='unknown-http'`).Scan(&status); err != nil || status != "historical_unknown" {
		t.Fatal("fallback manufactured HTTP result")
	}
}
