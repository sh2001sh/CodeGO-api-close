//go:build pgintegration

package channelmarket_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestModelInsightsFilterDeduplicateAndMeasure(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES($1,'other-model')`, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	empty, err := f.s.ModelInsights(ctx, channelmarket.Actor{}, c.ID, "fixture-model", 24)
	if err != nil || empty.RequestCount != 0 || empty.SuccessRate != nil || empty.TTFTP50MS != nil || empty.AverageTPS != nil || empty.Disclosure != nil {
		t.Fatalf("fabricated observations: %+v %v", empty, err)
	}
	now := time.Unix(f.now.Load(), 0)
	write := func(id, model, status string, counted bool, when time.Time, statusCode int64, ttft, generation *float64) {
		t.Helper()
		_, err := f.pool.Exec(ctx, `INSERT INTO v3_audit.request_audits(request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at,ttft_ms,generation_ms)
 VALUES($1,'',2,1,$2,'','openai','stream',$3,$4,false,0,0,100,$5,1,0,$6,'',$7,$7,$7,$7,$8,$9)`, id, model, status, counted, c.InternalChannelID, statusCode, when, ttft, generation)
		if err != nil {
			t.Fatal(err)
		}
	}
	ttft1, ttft2, generation := 100.0, 300.0, 2000.0
	write("measured-1", "fixture-model", "success", true, now.Add(-time.Hour), 200, &ttft1, &generation)
	write("measured-2", "fixture-model", "success", true, now.Add(-2*time.Hour), 200, &ttft2, &generation)
	write("timeout", "fixture-model", "failed", true, now.Add(-time.Hour), 504, nil, nil)
	write("excluded", "fixture-model", "cancelled", false, now.Add(-time.Hour), 499, nil, nil)
	write("other-model", "other-model", "failed", true, now.Add(-time.Hour), 500, nil, nil)
	write("old", "fixture-model", "success", true, now.Add(-25*time.Hour), 200, nil, nil)
	write("future", "fixture-model", "success", true, now.Add(time.Second), 200, nil, nil)
	write("left-boundary", "fixture-model", "success", true, now.Add(-24*time.Hour), 200, nil, nil)
	var wallet, subscription int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'subscription') RETURNING id`).Scan(&subscription); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES
 ($1,$2,2,$4,10,'measured-1','fixture-model','completed'),($1,$3,2,$4,20,'measured-1','fixture-model','completed'),
 ($1,$2,2,$4,10,'historical-split','fixture-model','completed'),($1,$3,2,$4,20,'historical-split','fixture-model','completed'),
 ($1,$2,2,$4,10,'excluded','fixture-model','completed')`, now.Add(-time.Hour), wallet, subscription, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.ModelInsights(ctx, channelmarket.Actor{}, c.GroupID, "fixture-model", 24)
	if err != nil || got.DisplayID != c.ID || got.RequestCount != 5 || got.SuccessCount != 4 || got.IndependentConsumers != 1 || got.PerformanceSamples != 2 {
		t.Fatalf("model audit dedup: %+v %v", got, err)
	}
	if got.SuccessRate == nil || *got.SuccessRate != 0.8 || got.WilsonSuccessRate == nil || *got.WilsonSuccessRate >= 0.8 || got.TTFTP50MS == nil || *got.TTFTP50MS != 200 || got.TTFTP95MS == nil || math.Abs(*got.TTFTP95MS-290) > 0.001 || got.AverageTPS == nil || *got.AverageTPS != 50 {
		t.Fatalf("real metrics incorrect: %+v", got)
	}
	if len(got.FailureCounts) != 1 || got.FailureCounts[0].Category != "timeout" || got.FailureCounts[0].Count != 1 {
		t.Fatalf("failure categories: %+v", got.FailureCounts)
	}
	week, err := f.s.ModelInsights(ctx, channelmarket.Actor{}, c.ID, "fixture-model", 168)
	if err != nil || week.RequestCount != 6 || week.SuccessCount != 5 {
		t.Fatalf("window not applied: %+v %v", week, err)
	}
	if err = f.s.SetBlock(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ModelInsights(ctx, channelmarket.Actor{UserID: 2}, c.ID, "fixture-model", 24); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("blocked consumer accessed metrics: %v", err)
	}
	private := f.channel(t, "private")
	f.active(t, private)
	if _, err = f.s.ModelInsights(ctx, channelmarket.Actor{}, private.ID, "fixture-model", 24); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("private insight leaked: %v", err)
	}
}

func TestDisclosureOwnershipUnknownsAndServerProvenance(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	f.active(t, c)
	owner := channelmarket.Actor{UserID: 1}
	if missing, err := f.s.Disclosure(ctx, owner, c.InternalChannelID); err != nil || missing != nil {
		t.Fatalf("missing declaration invented: %+v %v", missing, err)
	}
	in := channelmarket.ChannelMarketDisclosureInput{SourceKind: "direct", Regions: []string{"HK"}, Retention: "unknown", Training: "unknown", Models: []channelmarket.ChannelModelDisclosure{{Model: "fixture-model", Streaming: "supported"}}}
	if _, err := f.s.SaveDisclosure(ctx, channelmarket.Actor{UserID: 2}, c.InternalChannelID, in); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("cross-owner declaration write: %v", err)
	}
	if _, err := f.s.Disclosure(ctx, channelmarket.Actor{UserID: 2}, c.InternalChannelID); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("non-owner read: %v", err)
	}
	saved, err := f.s.SaveDisclosure(ctx, owner, c.InternalChannelID, in)
	if err != nil || saved.Provenance != "owner_declared" || saved.Models[0].Tools != "unknown" || saved.UpdatedAt.IsZero() {
		t.Fatalf("save declaration: %+v %v", saved, err)
	}
	insight, err := f.s.ModelInsights(ctx, channelmarket.Actor{}, c.ID, "fixture-model", 24)
	if err != nil || insight.Disclosure == nil || insight.Disclosure.Provenance != "owner_declared" {
		t.Fatalf("public declaration provenance: %+v %v", insight, err)
	}
	in.Models[0].Model = "not-configured"
	if _, err = f.s.SaveDisclosure(ctx, owner, c.InternalChannelID, in); !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("undeclared channel model accepted: %v", err)
	}
	mux := http.NewServeMux()
	f.s.RegisterInsightsHTTP(mux, func(*http.Request) (channelmarket.Actor, error) { return owner, nil })
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/marketplace/channels/"+c.ID+"/disclosure", strings.NewReader(`{"source_kind":"direct","provenance":"verified"}`))
	mux.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("provenance forgery: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPut, "/api/marketplace/channels/"+c.ID+"/disclosure", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://cross-site.example")
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site write accepted: %d", w.Code)
	}
	body, _ := json.Marshal(insight)
	if strings.Contains(string(body), "request_id") || strings.Contains(string(body), "user_id") || strings.Contains(string(body), "fixture-channel-secret") {
		t.Fatalf("insights leaked identities/secrets: %s", body)
	}
}
