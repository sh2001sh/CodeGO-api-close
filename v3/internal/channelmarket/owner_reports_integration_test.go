//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func reportFixtures(t *testing.T, f *fixture) (channelmarket.ChannelView, channelmarket.ChannelView, int64) {
	t.Helper()
	c := f.channel(t, "public")
	foreign, err := f.s.Create(ctx, 2, channelmarket.CreateRequest{Provider: "openai", BaseURL: "https://example.com", APIKey: "other-test-secret", Models: []string{"fixture-model"}, Visibility: "public"})
	if err != nil {
		t.Fatal(err)
	}
	var account int64
	if err = f.pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',2,'wallet') RETURNING id`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	return c, foreign, account
}

func TestOwnerReportsRetainTimestampTiesAndCompleteHistory(t *testing.T) {
	f := setup(t)
	c, foreign, account := reportFixtures(t, f)
	now := time.Unix(f.now.Load(), 0)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,prompt_tokens,completion_tokens,request_id,model,terminal)
	SELECT $1,$2,2,$3,3,3,5,'request-'||i,'=formula-model',
	CASE i WHEN 1 THEN 'completed_no_usage' WHEN 2 THEN 'Completed' WHEN 3 THEN 'CompletedNoUsage' ELSE 'completed' END
	FROM generate_series(1,1205) i`, now, account, c.InternalChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal) VALUES($1,$2,2,$3,900,'FOREIGN-SECRET','foreign-model','completed')`, now, account, foreign.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	a := channelmarket.Actor{UserID: 1}
	before, beforeID := time.Time{}, int64(0)
	seen := map[int64]bool{}
	for {
		items, e := f.s.LogsPage(ctx, a, before, beforeID, 97)
		if e != nil {
			t.Fatal(e)
		}
		for _, l := range items {
			if seen[l.ID] || l.ChannelID != c.InternalChannelID {
				t.Fatalf("duplicate/foreign log: %+v", l)
			}
			seen[l.ID] = true
		}
		if len(items) < 97 {
			break
		}
		last := items[len(items)-1]
		before, beforeID = last.CreatedAt, last.ID
	}
	if len(seen) != 1205 {
		t.Fatalf("timestamp cursor skipped rows: got %d", len(seen))
	}
	usage, err := f.s.UserUsage(ctx, a)
	if err != nil || len(usage) != 1 || usage[2].Requests != 1205 || usage[2].Amount != 3615 {
		t.Fatalf("usage truncated/foreign money included: %+v %v", usage, err)
	}
	var buffer bytes.Buffer
	if err = f.s.ExportLogs(ctx, a, csv.NewWriter(&buffer)); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buffer).ReadAll()
	if err != nil || len(rows) != 1206 || rows[1][4] != "'=formula-model" {
		t.Fatalf("CSV incomplete/unsafe: rows=%d err=%v", len(rows), err)
	}
	series, err := f.s.UserUsageSeries(ctx, a, c.InternalChannelID, 2, 24)
	if err != nil || series.ChannelID != c.ID || len(series.Points) != 1 || series.Points[0].Requests != 1205 || series.Points[0].Successes != 1205 || series.Points[0].Tokens != 9640 || series.Points[0].Amount != 3615 {
		t.Fatalf("usage series: %+v %v", series, err)
	}
	if _, err = f.s.UserUsageSeries(ctx, a, foreign.InternalChannelID, 2, 24); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("foreign series leaked: %v", err)
	}
	if _, err = f.s.UserUsageSeries(ctx, a, c.InternalChannelID, 2, 8761); !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("unbounded series accepted: %v", err)
	}
	mux := http.NewServeMux()
	f.s.Register(mux, func(*http.Request) (channelmarket.Actor, error) { return a, nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/channels/mine/logs?before_id=1", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("incomplete HTTP cursor accepted: %d", w.Code)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/channels/"+c.ID+"/user-usage/2/time-series?range_hours=24", nil))
	var envelope struct {
		Success bool                      `json:"success"`
		Data    channelmarket.UsageSeries `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != http.StatusOK || !envelope.Success || len(envelope.Data.Points) != 1 || envelope.Data.Points[0].Tokens != 9640 {
		t.Fatalf("time series route not wired: %d %s %v", w.Code, w.Body.String(), err)
	}
}

func TestOwnerReportAggregateOverflowIsAnError(t *testing.T) {
	f := setup(t)
	c, _, account := reportFixtures(t, f)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,request_id,model,terminal)
VALUES($1,$2,2,$3,$4,'big-value','model','completed'),($1,$2,2,$3,1,'one-more','model','completed')`, time.Unix(f.now.Load(), 0), account, c.InternalChannelID, int64(math.MaxInt64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UserUsage(ctx, channelmarket.Actor{UserID: 1}); err == nil {
		t.Fatal("aggregate amount overflow silently wrapped")
	}
	if _, err = f.s.UserUsageSeries(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, 2, 24); err == nil {
		t.Fatal("series amount overflow silently wrapped")
	}
}

func TestSecurityExportIncludesAllOwnedRowsWithoutRawDetails(t *testing.T) {
	f := setup(t)
	c, foreign, _ := reportFixtures(t, f)
	_, err := f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.security_audit_events(channel_id,actor_user_id,kind,details)
SELECT $1,1,'=owner-event','{"raw":"RAW-SECRET"}'::jsonb FROM generate_series(1,1205)`, c.InternalChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO v3_channelmarket.security_audit_events(channel_id,actor_user_id,kind,details) VALUES($1,2,'FOREIGN-SECRET','{}')`, foreign.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err = f.s.ExportSecurity(ctx, channelmarket.Actor{UserID: 1}, csv.NewWriter(&buffer)); err != nil {
		t.Fatal(err)
	}
	text := buffer.String()
	if strings.Contains(text, "RAW-SECRET") || strings.Contains(text, "FOREIGN-SECRET") {
		t.Fatal("security CSV exposed foreign event or raw details")
	}
	rows, err := csv.NewReader(&buffer).ReadAll()
	if err != nil || len(rows) < 1206 || rows[1][3] != "'=owner-event" || rows[1][1] != strconv.FormatInt(c.InternalChannelID, 10) {
		t.Fatalf("security CSV truncated or unsafe: rows %d err %v", len(rows), err)
	}
	buffer.Reset()
	if err = f.s.ExportSecurity(ctx, channelmarket.Actor{UserID: 3, Admin: true}, csv.NewWriter(&buffer)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buffer.String(), "FOREIGN-SECRET") {
		t.Fatal("admin export excluded authorized other owner")
	}
	items, err := f.s.SecurityPage(ctx, channelmarket.Actor{UserID: 1}, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(items)
	if err != nil || !bytes.Contains(body, []byte("RAW-SECRET")) {
		t.Fatal("test did not seed private detail")
	}
}
