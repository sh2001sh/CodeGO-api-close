//go:build pgintegration

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

type realtimeFundsFixture struct{ account, balance int64 }

func (f realtimeFundsFixture) WalletAccount(context.Context, int64) (int64, error) {
	return f.account, nil
}
func (f realtimeFundsFixture) LedgerBalance(context.Context, int64) (credits.Micro, int64, error) {
	return credits.Micro(f.balance), 0, nil
}
func (f realtimeFundsFixture) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: f.account, KeyID: f.account, Group: "default"}, nil
}

type realtimeFundsPlanner struct{ target gateway.Target }

func (p realtimeFundsPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{p.target}, nil
}
func (realtimeFundsPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type realtimeFundsRepository struct{}

func (realtimeFundsRepository) Put(context.Context, live.Locator, time.Duration) error { return nil }
func (realtimeFundsRepository) Get(context.Context, string, int64, int64) (live.Locator, error) {
	return live.Locator{}, live.ErrNotFound
}
func (realtimeFundsRepository) Delete(context.Context, string, int64, int64) error { return nil }

func TestRealtimeFundsAreSettledBeforeNextTurnAndIdleHoldIsRefunded(t *testing.T) {
	addr := os.Getenv("V3_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("V3_TEST_REDIS_ADDR not configured")
	}
	for _, funds := range []int64{5000, 20_000} {
		t.Run(fmt.Sprint(funds), func(t *testing.T) {
			ctx := context.Background()
			rdb, err := redisx.Connect(redisx.Config{Addr: addr})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rdb.Close() }()
			fixture := realtimeFundsFixture{account: time.Now().UnixNano(), balance: funds}
			snapshot := &catalog.Snapshot{
				Groups:          map[string]catalog.Group{"default": {Name: "default", Multiplier: 1}},
				Prices:          map[string]catalog.Price{"gpt-realtime": {Model: "gpt-realtime", Mode: "per_token", OutputPerMTok: 1_000_000}},
				AccountProfiles: map[int64]catalog.AccountProfile{fixture.account: {WalletAccountID: fixture.account}},
			}
			settler, err := billing.New(rdb, func() *catalog.Snapshot { return snapshot }, fixture, fixture, billing.Config{DisableOutageAdmission: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer settler.Close()
			var generations atomic.Int32
			upstream := httptest.NewServer(websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: websocket.Handler(func(conn *websocket.Conn) {
				defer func() { _ = conn.Close() }()
				for {
					var frame string
					if websocket.Message.Receive(conn, &frame) != nil {
						return
					}
					id := generations.Add(1)
					message := fmt.Sprintf(`{"type":"response.done","response":{"id":"funds-%d","output":[{"type":"message"}],"usage":{"input_tokens":20,"output_tokens":3000}}}`, id)
					if websocket.Message.Send(conn, message) != nil {
						return
					}
				}
			})})
			defer upstream.Close()
			target := gateway.Target{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL}
			handler, err := live.New(live.Config{Auth: fixture, Planner: realtimeFundsPlanner{target}, Settler: settler,
				Repository: realtimeFundsRepository{}, Resolve: func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }, SessionTimeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			handler.Register(mux)
			server := httptest.NewServer(mux)
			defer server.Close()
			cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime?model=gpt-realtime", server.URL)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Header.Set("Authorization", "Bearer local-test")
			conn, err := cfg.DialContext(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			wantRounds := int32(3)
			if funds == 5000 {
				wantRounds = 1
			}
			for i := int32(0); i < wantRounds; i++ {
				if err := websocket.Message.Send(conn, `{"type":"response.create"}`); err != nil {
					t.Fatal(err)
				}
				var message string
				if err := websocket.Message.Receive(conn, &message); err != nil {
					t.Fatal(err)
				}
				if gjson.Get(message, "type").Str != "response.done" {
					t.Fatalf("terminal response lost: %s", message)
				}
				if balance, _ := rdb.HGet(ctx, billing.BalanceKey(fixture.account), "balance").Int64(); balance != funds-int64(i+1)*3000 {
					t.Fatalf("turn not settled before completion: balance=%d", balance)
				}
			}
			if funds == 5000 {
				var message string
				if err := websocket.Message.Receive(conn, &message); err != nil || gjson.Get(message, "status").Int() != 402 {
					t.Fatalf("insufficient next turn not refused: %s err=%v", message, err)
				}
			}
			_ = conn.Close()
			held := int64(-1)
			for range 100 {
				held, err = rdb.HGet(ctx, billing.BalanceKey(fixture.account), "reserved").Int64()
				if err != nil {
					t.Fatal(err)
				}
				if held == 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			balance, err := rdb.HGet(ctx, billing.BalanceKey(fixture.account), "balance").Int64()
			if err != nil || generations.Load() != wantRounds || balance != funds-int64(wantRounds)*3000 || held != 0 {
				t.Fatalf("generations=%d balance=%d held=%d err=%v", generations.Load(), balance, held, err)
			}
			t.Logf("REAL REDIS: starting=%d accepted_rounds=%d charged=%d balance=%d idle_hold=%d", funds, wantRounds, funds-balance, balance, held)
		})
	}
}
