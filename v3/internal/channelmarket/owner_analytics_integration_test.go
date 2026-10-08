//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"encoding/csv"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func seedOwnerSettlement(t *testing.T, f *fixture, c channelmarket.ChannelView, request string, created time.Time, consumer, gross, commission, fee, net, reclaimed int64, state string) {
	t.Helper()
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.settlements(id,request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,commission_micro,fee_micro,net_micro,reclaimed_micro,status,available_at,created_at)
VALUES($1,$1,$2,$3,2,'wallet',$4,$5,$6,$7,$8,$9,$10,$11,$12)`, request, c.InternalChannelID, c.OwnerUserID, consumer, gross, commission, fee, net, reclaimed, state, created.Add(24*time.Hour), created)
	if err != nil {
		t.Fatal(err)
	}
}

func seedOwnerAudit(t *testing.T, f *fixture, c channelmarket.ChannelView, request string, created time.Time, successful, counted bool, user, prompt, completion int64) {
	t.Helper()
	status := "failed"
	if successful {
		status = "success"
	}
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_audit.request_audits(request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,error_code,started_at,completed_at,created_at,updated_at)
VALUES($1,$1,$2,0,'fixture-model','market','openai','chat',$3,$4,false,0,$5,$6,$7,1,0,200,'',$8,$8,$8,$8)`, request, user, status, counted, prompt, completion, c.InternalChannelID, created)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOwnerAnalyticsUsesRealRequestsAndKeepsMissingUsageSettlements(t *testing.T) {
	f := setup(t)
	c, foreign, account := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	from := to.Add(-24 * time.Hour)
	stamp := from.Add(time.Hour)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,prompt_tokens,completion_tokens,request_id,model,terminal)
VALUES($1,$2,2,$3,500,10,20,'billed-fallback','fixture-model','completed'),
($4,$2,2,$3,400,10,20,'billed-fallback','fixture-model','completed'),
($1,$2,3,$3,300,999,999,'audited','fixture-model','completed')`, stamp, account, c.InternalChannelID, stamp.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	seedOwnerAudit(t, f, c, "audited", stamp, true, true, 3, 30, 40)
	seedOwnerAudit(t, f, c, "failed", stamp, false, true, 2, 0, 0)
	seedOwnerAudit(t, f, c, "user-input-error", stamp, false, false, 2, 0, 0)
	seedOwnerSettlement(t, f, c, "billed-fallback", from, 900, 1000, 50, 0, 950, 0, "pending")
	seedOwnerSettlement(t, f, c, "missing-usage", stamp, 2000, 2000, 100, 20, 1880, 80, "released")
	seedOwnerSettlement(t, f, c, "audited", stamp, 300, 500, 25, 0, 475, 0, "released")
	seedOwnerSettlement(t, f, c, "outside-end", to, 10, 10, 0, 0, 10, 0, "pending")
	seedOwnerSettlement(t, f, foreign, "FOREIGN-SECRET", stamp, 10, 10, 0, 0, 10, 0, "pending")
	a := channelmarket.Actor{UserID: 1}
	filter := channelmarket.OwnerAnalyticsFilter{From: from, To: to}
	r, err := f.s.OwnerAnalytics(ctx, a, filter)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Summary
	if s.RequestCount != 3 || s.SuccessCount != 2 || s.ConsumerCount != 2 || s.PromptTokens != 40 || s.CompletionTokens != 60 {
		t.Fatalf("requests duplicated/failed outcomes lost/audit tokens ignored: %+v", s)
	}
	if s.ConsumerMicro != 3200 || s.GrossMicro != 3500 || s.CommissionMicro != 175 || s.FeeMicro != 20 || s.NetMicro != 3305 || s.PendingIncomeMicro != 950 || s.ReleasedIncomeMicro != 2275 || s.ReclaimedIncomeMicro != 80 || s.NextAvailableAt == nil || !s.NextAvailableAt.Equal(from.Add(24*time.Hour)) {
		t.Fatalf("ledger summary inconsistent or foreign/time boundary leaked: %+v", s)
	}
	if len(r.Settlements) != 3 || r.SettlementsTruncated || len(r.Channels) != 2 {
		t.Fatalf("missing-usage settlement/contribution lost: %+v", r)
	}
	var pointGross, pointRequests, channelGross int64
	for _, p := range r.Points {
		pointGross += p.GrossMicro
		pointRequests += p.RequestCount
	}
	for _, ch := range r.Channels {
		if ch.ChannelID != c.ID {
			t.Fatalf("internal/foreign ID exposed: %+v", ch)
		}
		channelGross += ch.GrossMicro
	}
	if pointGross != s.GrossMicro || pointRequests != s.RequestCount || channelGross != s.GrossMicro {
		t.Fatalf("series/contribution do not reconcile: %d %d %d", pointGross, pointRequests, channelGross)
	}
	filter.Model = "fixture-model"
	r, err = f.s.OwnerAnalytics(ctx, a, filter)
	if err != nil || r.Summary.GrossMicro != 1500 || len(r.Settlements) != 2 || r.Summary.RequestCount != 3 {
		t.Fatalf("model filter ignored/unknown model fabricated: %+v %v", r, err)
	}
	filter.ChannelID = foreign.ID
	if _, err = f.s.OwnerAnalytics(ctx, a, filter); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("foreign selection authorized: %v", err)
	}
	var buffer bytes.Buffer
	if err = f.s.ExportOwnerAnalytics(ctx, a, filter, csv.NewWriter(&buffer)); !errors.Is(err, channelmarket.ErrNotFound) || buffer.Len() != 0 {
		t.Fatalf("foreign export authorized/partial data leaked: %v %s", err, buffer.String())
	}
	filter.ChannelID, filter.Model = c.ID, "unused-model"
	r, err = f.s.OwnerAnalytics(ctx, a, filter)
	if err != nil || r.Summary.RequestCount != 0 || r.Summary.NextAvailableAt != nil || len(r.Points) != 0 || len(r.Settlements) != 0 || len(r.Channels) != 0 {
		t.Fatalf("empty result fabricated samples: %+v %v", r, err)
	}
}

func TestOwnerAnalyticsMoneyOverflowIsAnError(t *testing.T) {
	f := setup(t)
	c, _, _ := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	stamp := to.Add(-time.Hour)
	seedOwnerSettlement(t, f, c, "max-value", stamp, math.MaxInt64, math.MaxInt64, 0, 0, math.MaxInt64, 0, "pending")
	seedOwnerSettlement(t, f, c, "one-more", stamp, 1, 1, 0, 0, 1, 0, "pending")
	if _, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, channelmarket.OwnerAnalyticsFilter{From: stamp.Add(-time.Hour), To: to}); err == nil {
		t.Fatal("financial aggregate overflow silently wrapped")
	}
}

func TestOwnerAnalyticsExcludedAuditsStillSuppressSplitUsageFallback(t *testing.T) {
	f := setup(t)
	c, foreign, account := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	from := to.Add(-24 * time.Hour)
	stamp := from.Add(time.Hour)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,prompt_tokens,completion_tokens,request_id,model,terminal)
SELECT $1::timestamptz+(split-1)*interval '1 second',$2,2,$3,50,10,20,request_id,'fixture-model','completed'
FROM unnest(ARRAY['historical-audit','excluded-outcome','unfinished-audit','different-model','foreign-audit','no-audit']) request_id
CROSS JOIN generate_series(1,2) split`, stamp, account, c.InternalChannelID)
	if err != nil {
		t.Fatal(err)
	}
	seedOwnerAudit(t, f, c, "historical-audit", from.Add(-time.Hour), true, true, 2, 10, 20)
	seedOwnerAudit(t, f, c, "excluded-outcome", stamp, false, false, 2, 10, 20)
	seedOwnerAudit(t, f, c, "unfinished-audit", stamp, true, true, 2, 10, 20)
	seedOwnerAudit(t, f, c, "different-model", stamp, true, true, 2, 10, 20)
	seedOwnerAudit(t, f, foreign, "foreign-audit", stamp, true, true, 2, 10, 20)
	if _, err = f.pool.Exec(ctx, `UPDATE v3_audit.request_audits SET completed_at=$1 WHERE request_id='unfinished-audit'`, to.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_audit.request_audits SET model='other-model' WHERE request_id='different-model'`); err != nil {
		t.Fatal(err)
	}
	filter := channelmarket.OwnerAnalyticsFilter{From: from, To: to, Model: "fixture-model"}
	r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.SuccessCount != 1 || r.Summary.PromptTokens != 10 || r.Summary.CompletionTokens != 20 || len(r.Channels) != 1 || r.Channels[0].ChannelID != c.ID {
		t.Fatalf("excluded audit fell back to split usage or legacy request duplicated: %+v %v", r, err)
	}
	filter.Model = ""
	r, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 2 || r.Summary.PromptTokens != 20 || r.Summary.CompletionTokens != 40 {
		t.Fatalf("unfiltered audit precedence lost: %+v %v", r, err)
	}
}

func TestOwnerAnalyticsRetainsOutOfRangeUsageModelsAndAuditPrecedence(t *testing.T) {
	f := setup(t)
	c, foreign, account := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	from := to.Add(-24 * time.Hour)
	stamp := from.Add(time.Hour)
	model := "retained-'模型;--"
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal)
VALUES($1,$2,2,$3,100,'retained-usage',$4,'completed'),
($5,$2,2,$3,100,'audit-wins',$4,'completed')`, from.Add(-time.Hour), account, c.InternalChannelID, model, stamp)
	if err != nil {
		t.Fatal(err)
	}
	seedOwnerSettlement(t, f, c, "retained-usage", stamp, 100, 100, 5, 0, 95, 0, "pending")
	seedOwnerSettlement(t, f, c, "audit-wins", stamp, 100, 100, 5, 0, 95, 0, "released")
	seedOwnerAudit(t, f, c, "audit-wins", stamp, true, true, 2, 10, 20)
	seedOwnerSettlement(t, f, foreign, "foreign-history", stamp, 100, 100, 5, 0, 95, 0, "pending")
	filter := channelmarket.OwnerAnalyticsFilter{From: from, To: to, Model: model}
	for i := 0; i < 8; i++ {
		r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
		if err != nil || r.Summary.RequestCount != 0 || r.Summary.GrossMicro != 100 || len(r.Settlements) != 1 || r.Settlements[0].Model != model {
			t.Fatalf("retained model missing, audit overridden, or owner filter leaked on read %d: %+v %v", i, r, err)
		}
	}
	filter.Model = "fixture-model"
	r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.GrossMicro != 100 || len(r.Settlements) != 1 || r.Settlements[0].ID != "audit-wins" {
		t.Fatalf("audit model precedence lost: %+v %v", r, err)
	}
	filter.Model = ""
	r, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{Admin: true}, filter)
	if err != nil || r.Summary.GrossMicro != 300 || len(r.Settlements) != 3 {
		t.Fatalf("admin aggregate accidentally owner-scoped: %+v %v", r, err)
	}
}

func TestOwnerAnalyticsExportIsCompleteAndReportsApplyIdenticalFilters(t *testing.T) {
	f := setup(t)
	c, foreign, account := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	from := to.Add(-24 * time.Hour)
	stamp := from.Add(time.Hour)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.settlements(id,request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,commission_micro,fee_micro,net_micro,available_at,created_at)
SELECT 'settlement-'||i,'settlement-'||i,$1,1,2,'wallet',100,100,5,0,95,$2,$3 FROM generate_series(1,1205) i`, c.InternalChannelID, stamp.Add(24*time.Hour), stamp)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal)
SELECT $1,$2,2,$3,100,'settlement-'||i,'=formula-model','completed' FROM generate_series(1,1205) i`, stamp, account, c.InternalChannelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal)
VALUES($1,$2,2,$3,1,'outside-end','other-model','completed'),($4,$2,2,$5,1,'FOREIGN-SECRET','other-model','completed')`, to, account, c.InternalChannelID, stamp, foreign.InternalChannelID)
	if err != nil {
		t.Fatal(err)
	}
	a := channelmarket.Actor{UserID: 1}
	filter := channelmarket.OwnerAnalyticsFilter{From: from, To: to, ChannelID: c.ID, Model: "=formula-model"}
	r, err := f.s.OwnerAnalytics(ctx, a, filter)
	if err != nil || len(r.Settlements) != 100 || !r.SettlementsTruncated || r.Summary.NetMicro != 1205*95 {
		t.Fatalf("preview/aggregate truncated: %+v %v", r, err)
	}
	var buffer bytes.Buffer
	if err = f.s.ExportOwnerAnalytics(ctx, a, filter, csv.NewWriter(&buffer)); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buffer).ReadAll()
	if err != nil || len(rows) != 1206 || rows[1][4] != "'=formula-model" || strings.Contains(buffer.String(), "FOREIGN-SECRET") {
		t.Fatalf("export incomplete/unsafe: rows=%d %v", len(rows), err)
	}
	items, err := f.s.LogsPageFiltered(ctx, a, time.Time{}, 0, 97, filter)
	if err != nil || len(items) != 97 {
		t.Fatalf("filtered log page: %d %v", len(items), err)
	}
	last := items[len(items)-1]
	next, err := f.s.LogsPageFiltered(ctx, a, last.CreatedAt, last.ID, 97, filter)
	if err != nil || len(next) != 97 || next[0].ID >= last.ID {
		t.Fatalf("filtered timestamp tie cursor skipped/duplicated: %v", err)
	}
	usage, err := f.s.UserUsageFiltered(ctx, a, filter)
	if err != nil || usage[2].Requests != 1205 || usage[2].Amount != 120500 {
		t.Fatalf("filtered usage: %+v %v", usage, err)
	}
	mux := http.NewServeMux()
	f.s.RegisterOwnerAnalyticsHTTP(mux, func(*http.Request) (channelmarket.Actor, error) { return a, nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/channels/mine/analytics?channel_id="+foreign.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("foreign HTTP selection not rejected: %d %s", w.Code, w.Body.String())
	}
}
