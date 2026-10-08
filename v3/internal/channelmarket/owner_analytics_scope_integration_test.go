//go:build pgintegration

package channelmarket_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestOwnerAnalyticsReadsCurrentOwnershipAndSettlementOwner(t *testing.T) {
	f := setup(t)
	c, _, _ := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	stamp := to.Add(-time.Hour)
	seedOwnerAudit(t, f, c, "ownership-change", stamp, true, true, 3, 10, 20)
	seedOwnerSettlement(t, f, c, "ownership-change", stamp, 100, 100, 5, 0, 95, 0, "pending")
	filter := channelmarket.OwnerAnalyticsFilter{From: to.Add(-24 * time.Hour), To: to}
	r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.NetMicro != 95 {
		t.Fatalf("original owner: %+v %v", r.Summary, err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE v3_channelmarket.groups SET owner_user_id=2 WHERE channel_id=$1`, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	r, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 0 || r.Summary.NetMicro != 0 || len(r.Points) != 0 || len(r.Channels) != 0 || len(r.Settlements) != 0 {
		t.Fatalf("former owner retained report data: %+v %v", r, err)
	}
	r, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 2}, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.NetMicro != 0 || len(r.Settlements) != 0 {
		t.Fatalf("new owner inherited immutable old income: %+v %v", r, err)
	}
	r, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{Admin: true}, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.NetMicro != 95 {
		t.Fatalf("admin lost historical settlement: %+v %v", r.Summary, err)
	}
	filter.ChannelID = c.ID
	if _, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter); !errors.Is(err, channelmarket.ErrNotFound) {
		t.Fatalf("former owner explicit selection allowed: %v", err)
	}
}

func TestOwnerAnalyticsCancellationDoesNotPoisonFollowingRequest(t *testing.T) {
	f := setup(t)
	c, _, _ := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	stamp := to.Add(-time.Hour)
	seedOwnerSettlement(t, f, c, "cancel-report", stamp, 100, 100, 5, 0, 95, 0, "pending")
	filter := channelmarket.OwnerAnalyticsFilter{From: to.Add(-24 * time.Hour), To: to}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.s.OwnerAnalytics(cancelled, channelmarket.Actor{UserID: 1}, filter); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled report continued: %v", err)
	}
	r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.NetMicro != 95 || len(r.Settlements) != 1 {
		t.Fatalf("cancellation leaked connection or result: %+v %v", r, err)
	}
}

func TestOwnerAnalyticsEmptyAuditModelDoesNotUseHistoricalModel(t *testing.T) {
	f := setup(t)
	c, _, account := reportFixtures(t, f)
	to := time.Unix(f.now.Load(), 0).UTC()
	stamp := to.Add(-time.Hour)
	seedOwnerAudit(t, f, c, "empty-audit-model", stamp, true, true, 2, 10, 20)
	seedOwnerSettlement(t, f, c, "empty-audit-model", stamp, 100, 100, 5, 0, 95, 0, "pending")
	if _, err := f.pool.Exec(ctx, `UPDATE v3_audit.request_audits SET model='' WHERE request_id='empty-audit-model'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO v3_billing.usage_logs(created_at,account_id,user_id,channel_id,amount,prompt_tokens,completion_tokens,request_id,model,terminal)
	VALUES($1,$2,2,$3,100,999,999,'empty-audit-model','historical-model','completed')`, stamp, account, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	filter := channelmarket.OwnerAnalyticsFilter{From: to.Add(-24 * time.Hour), To: to}
	r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.PromptTokens != 10 || r.Summary.NetMicro != 95 || len(r.Channels) != 1 || r.Channels[0].Model != "" || len(r.Settlements) != 1 || r.Settlements[0].Model != "" {
		t.Fatalf("empty audit model lost precedence over usage: %+v %v", r, err)
	}
	filter.Model = "historical-model"
	r, err = f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
	if err != nil || r.Summary.RequestCount != 0 || r.Summary.NetMicro != 0 || len(r.Points) != 0 || len(r.Channels) != 0 || len(r.Settlements) != 0 {
		t.Fatalf("historical model bypassed retained audit: %+v %v", r, err)
	}
}

func TestOwnerAnalyticsBucketsUseUTCEpochForHistoricalAndDailyRanges(t *testing.T) {
	f := setup(t)
	c, _, _ := reportFixtures(t, f)
	stamp := time.Date(1969, 12, 31, 23, 45, 0, 0, time.UTC)
	seedOwnerAudit(t, f, c, "before-epoch", stamp, true, true, 2, 10, 20)
	seedOwnerSettlement(t, f, c, "before-epoch", stamp, 100, 100, 5, 0, 95, 0, "pending")
	for _, span := range []time.Duration{time.Hour, 8 * 24 * time.Hour} {
		filter := channelmarket.OwnerAnalyticsFilter{From: stamp.Add(-span), To: stamp.Add(span)}
		r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, filter)
		wantBucket, wantStart := int64(3600), time.Date(1969, 12, 31, 23, 0, 0, 0, time.UTC)
		if span > time.Hour {
			wantBucket, wantStart = 86400, time.Date(1969, 12, 31, 0, 0, 0, 0, time.UTC)
		}
		if err != nil || r.BucketSeconds != wantBucket || len(r.Points) != 1 || !r.Points[0].Timestamp.Equal(wantStart) || r.Points[0].RequestCount != 1 || r.Points[0].NetMicro != 95 {
			t.Fatalf("epoch bucket mismatch for %s: %+v %v", span, r, err)
		}
	}
}

func TestOwnerAnalyticsConsumersStayDistinctAcrossBucketsChannelsAndModels(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	other := f.channel(t, "public")
	to := time.Unix(f.now.Load(), 0).UTC()
	stamp := to.Add(-3 * time.Hour)
	seedOwnerAudit(t, f, c, "consumer-early", stamp, true, true, 2, 10, 20)
	seedOwnerAudit(t, f, c, "consumer-late", stamp.Add(time.Hour), true, true, 2, 10, 20)
	seedOwnerAudit(t, f, other, "consumer-other-channel", stamp, true, true, 2, 10, 20)
	seedOwnerAudit(t, f, other, "another-consumer", stamp, true, true, 3, 10, 20)
	seedOwnerAudit(t, f, other, "another-model", stamp.Add(time.Hour), false, true, 3, 10, 20)
	if _, err := f.pool.Exec(ctx, `UPDATE v3_audit.request_audits SET model='other-model' WHERE request_id='another-model'`); err != nil {
		t.Fatal(err)
	}
	r, err := f.s.OwnerAnalytics(ctx, channelmarket.Actor{UserID: 1}, channelmarket.OwnerAnalyticsFilter{From: to.Add(-24 * time.Hour), To: to})
	if err != nil || r.Summary.RequestCount != 5 || r.Summary.SuccessCount != 4 || r.Summary.ConsumerCount != 2 || r.Summary.PromptTokens != 50 || r.Summary.CompletionTokens != 100 || len(r.Points) != 2 || len(r.Channels) != 3 {
		t.Fatalf("grouped request summary changed: %+v %v", r, err)
	}
	if r.Points[0].RequestCount != 3 || r.Points[0].SuccessCount != 3 || r.Points[1].RequestCount != 2 || r.Points[1].SuccessCount != 1 {
		t.Fatalf("bucket totals changed: %+v", r.Points)
	}
	consumers := map[string]int64{}
	for _, ch := range r.Channels {
		consumers[ch.ChannelID+"/"+ch.Model] = ch.ConsumerCount
	}
	if consumers[c.ID+"/fixture-model"] != 1 || consumers[other.ID+"/fixture-model"] != 2 || consumers[other.ID+"/other-model"] != 1 {
		t.Fatalf("channel/model consumers changed: %+v", r.Channels)
	}
}

func TestOwnerAnalyticsModelFilterPrecedesPreviewLimit(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	to := time.Unix(f.now.Load(), 0).UTC()
	older, newer := to.Add(-3*time.Hour), to.Add(-time.Hour)
	seedOwnerAudit(t, f, c, "rare-older-model", older, true, true, 2, 10, 20)
	seedOwnerSettlement(t, f, c, "rare-older-model", older, 100, 100, 5, 0, 95, 0, "pending")
	if _, err := f.pool.Exec(ctx, `UPDATE v3_audit.request_audits SET model='rare-model' WHERE request_id='rare-older-model'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		request := "newer-model-" + strconv.Itoa(i)
		seedOwnerAudit(t, f, c, request, newer, true, true, 2, 10, 20)
		seedOwnerSettlement(t, f, c, request, newer, 100, 100, 5, 0, 95, 0, "pending")
	}
	actor := channelmarket.Actor{UserID: 1}
	filter := channelmarket.OwnerAnalyticsFilter{From: to.Add(-24 * time.Hour), To: to}
	r, err := f.s.OwnerAnalytics(ctx, actor, filter)
	if err != nil || r.Summary.RequestCount != 121 || r.Summary.NetMicro != 121*95 || len(r.Settlements) != 100 || !r.SettlementsTruncated {
		t.Fatalf("unfiltered preview or totals changed: %+v %v", r, err)
	}
	filter.Model = "rare-model"
	r, err = f.s.OwnerAnalytics(ctx, actor, filter)
	if err != nil || r.Summary.RequestCount != 1 || r.Summary.NetMicro != 95 || len(r.Settlements) != 1 || r.SettlementsTruncated || r.Settlements[0].RequestID != "rare-older-model" {
		t.Fatalf("model matched after preview truncation: %+v %v", r, err)
	}
	var buffer bytes.Buffer
	if err = f.s.ExportOwnerAnalytics(ctx, actor, filter, csv.NewWriter(&buffer)); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buffer).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][1] != "rare-older-model" || rows[1][4] != "rare-model" {
		t.Fatalf("filtered export changed: %+v %v", rows, err)
	}
}
