//go:build pgintegration

package desktop

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDesktopHistoricalUnknownPreservesUsageObservations(t *testing.T) {
	s, user, _ := desktopFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	s.cfg.Now = func() time.Time { return now }
	stamp := now.Add(-time.Hour)
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('default')`); err != nil {
		t.Fatal(err)
	}
	var channel, account int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider) VALUES('historical-status','openai') RETURNING id`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,'default')`, channel); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,'fixture-model')`, channel); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') RETURNING id`, user.ID).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_audit.request_audits(request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at)
	 VALUES('unknown-desktop','',$1,0,'fixture-model','default','openai','stream','historical_unknown',false,false,0,0,0,$2,0,0,0,'',$3,'0001-01-01T00:00:00Z',$3,$3)`, user.ID, channel, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal)
	 VALUES($1,$2,$3,$4,10,'unknown-desktop','fixture-model','completed'),($1+interval '1 second',$2,$3,$4,5,'unknown-desktop','fixture-model','completed')`, stamp, account, user.ID, channel); err != nil {
		t.Fatal(err)
	}
	got, err := s.statusStats(httptest.NewRequest("GET", "/", nil), user.ID, now.Add(-2*time.Hour).Unix(), now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	var requests, successes int64
	for _, v := range got.buckets {
		requests += v[0]
		successes += v[1]
	}
	if requests != 1 || successes != 1 {
		t.Fatalf("usage hidden or split counted twice requests=%d successes=%d", requests, successes)
	}
}
