//go:build pgintegration

package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestMarketChannelAdminDeletePreservesHistoryAndHidesDeletedRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username) VALUES(1,'channel-owner')`); err != nil {
		t.Fatal(err)
	}
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	market := channelmarket.New(pool, enc, nil, channelmarket.Config{}, nil)
	c, err := market.Create(ctx, 1, channelmarket.CreateRequest{Name: "Owner channel", Provider: "openai", BaseURL: "https://example.com", APIKey: "hidden-fixture", Models: []string{"fixture-model"}, Visibility: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.QueueVerification(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := New(pool, enc, nil)
	server.Register(mux, func(h http.Handler) http.Handler { return h })
	server.recordMarketProbe(ctx, c.InternalChannelID, "fixture-model", time.Now(), probeResult{Time: .25})
	var samples, usage int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_channelmarket.verification_runs WHERE channel_id=$1 AND trigger='admin_probe' AND status='failed'),(SELECT count(*) FROM v3_billing.usage_logs)`, c.InternalChannelID).Scan(&samples, &usage); err != nil || samples != 1 || usage != 0 {
		t.Fatalf("admin probe history missing or charged: samples=%d usage=%d %v", samples, usage, err)
	}
	call := func(method, path string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != expected {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	call("DELETE", fmt.Sprintf("/api/catalog/channels/%d", c.InternalChannelID), 200)
	var status, runStatus string
	var retired bool
	if err := pool.QueryRow(ctx, `SELECT c.status,g.deleted_at IS NOT NULL,v.status FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id JOIN v3_channelmarket.verification_runs v ON v.channel_id=c.id AND v.trigger='owner' WHERE c.id=$1`, c.InternalChannelID).Scan(&status, &retired, &runStatus); err != nil || status != "disabled" || !retired || runStatus != "paused" {
		t.Fatalf("history lost or routing/probe still enabled: %s %t %s %v", status, retired, runStatus, err)
	}
	call("GET", fmt.Sprintf("/api/catalog/channels/%d", c.InternalChannelID), 404)
	for _, path := range []string{"/api/catalog/channels", "/api/channel/", "/api/channel/search?keyword=Owner"} {
		w := call("GET", path, 200)
		if strings.Contains(w.Body.String(), "Owner channel") || !strings.Contains(w.Body.String(), `"total":0`) {
			t.Fatalf("deleted channel is still listed: %s", w.Body.String())
		}
	}
	call("DELETE", fmt.Sprintf("/api/catalog/channels/%d", c.InternalChannelID), 404)
}

func TestChannelSearchFiltersBeforePagination(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES('rare'); INSERT INTO v3_catalog.channels(name,provider,status,tag) SELECT 'irrelevant-'||n,'openai','enabled','other' FROM generate_series(1,45) n`); err != nil {
		t.Fatal(err)
	}
	var wanted int64
	if err := pool.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider,status,tag) VALUES('target-channel','claude','disabled','special') RETURNING id`).Scan(&wanted); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES($1,'rare')`, wanted); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, nil, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	for _, prefix := range []string{"/api/catalog/channels", "/api/channel/search"} {
		for _, query := range []string{"group=rare&status=disabled&tag=special&provider=claude&page_size=1", fmt.Sprintf("keyword=%d&page_size=1", wanted), "keyword=special&page_size=1"} {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", prefix+"?"+query, nil))
			var result struct {
				Data struct {
					Total int
					Items []json.RawMessage
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Data.Total != 1 || len(result.Data.Items) != 1 || !bytes.Contains(result.Data.Items[0], []byte("target-channel")) {
				t.Fatalf("filter applied after pagination: %s %s %v", prefix, w.Body.String(), err)
			}
		}
	}
}
