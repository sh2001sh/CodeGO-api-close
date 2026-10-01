package live

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestResponsesGuardTypedErrorsDoNotAdmitGeneration(t *testing.T) {
	for _, denial := range guardDenials() {
		t.Run(denial.name, func(t *testing.T) {
			var calls, checks atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer upstream.Close()
			ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
			target := gateway.Target{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL}
			h, server, _ := socketFixture(t, []gateway.Target{target}, ledger)
			planner := &guardPlanner{livePlan: livePlan{targets: []gateway.Target{target}}}
			h.cfg.Planner = planner
			h.cfg.RequestGuard = func(ctx context.Context, req *gateway.Request) error {
				checks.Add(1)
				if ctx.Err() != nil || req.ID == "" || req.Principal.UserID != 1 || req.Principal.KeyID != 11 || req.Path != "/backend-api/codex/responses" || req.Model != "gpt-test" {
					t.Error("guard lost authenticated socket generation")
				}
				return denial.err
			}
			conn := dialSocket(t, server.URL, "/backend-api/codex/responses")
			if err := websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","input":"denied"}`); err != nil {
				t.Fatal(err)
			}
			var message wireFrame
			if err := frameCodec.Receive(conn, &message); err != nil {
				t.Fatal(err)
			}
			if gjson.GetBytes(message.data, "status").Int() != int64(denial.want.Status) || gjson.GetBytes(message.data, "error.type").Str != denial.want.Type || gjson.GetBytes(message.data, "error.code").Str != denial.want.Code || gjson.GetBytes(message.data, "error.message").Str != denial.want.Message {
				t.Fatalf("socket guard error changed: %s", message.data)
			}
			if checks.Load() != 1 || planner.plans.Load() != 0 || ledger.reserves.Load() != 0 || calls.Load() != 0 {
				t.Fatalf("denied frame admitted generation checks=%d plans=%d reserves=%d upstream=%d", checks.Load(), planner.plans.Load(), ledger.reserves.Load(), calls.Load())
			}
		})
	}
}

func TestResponsesGuardCountsDistinctGenerationsOnceAcrossRetry(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, `{"error":{"message":"retry"}}`)
			return
		}
		testResponseSSE(w, fmt.Sprintf("resp_guard_%d", call))
	}))
	defer upstream.Close()
	first := gateway.Target{ChannelID: 1, CredentialID: 10, Provider: "openai", BaseURL: upstream.URL}
	second := first
	second.ChannelID, second.CredentialID = 2, 20
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2)}
	h, server, _ := socketFixture(t, []gateway.Target{first, second}, ledger)
	planner := &guardPlanner{livePlan: livePlan{targets: []gateway.Target{first, second}}}
	h.cfg.Planner = planner
	admitted := make(chan string, 4)
	h.cfg.RequestGuard = func(ctx context.Context, req *gateway.Request) error {
		if ctx.Err() != nil || req.ID == "" || req.Path != "/responses" || req.Principal.UserID != 1 {
			t.Error("guard lost request identity/path")
		}
		admitted <- req.ID
		return nil
	}
	conn := dialSocket(t, server.URL, "/responses")
	if err := websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","input":"warm","generate":false}`); err != nil {
		t.Fatal(err)
	}
	readSocketCompleted(t, conn)
	if err := websocket.Message.Send(conn, `{"type":"session.update","session":{"model":"gpt-test"}}`); err != nil {
		t.Fatal(err)
	}
	var metadata wireFrame
	if err := frameCodec.Receive(conn, &metadata); err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(metadata.data, "type").Str != "error" || len(admitted) != 0 || planner.plans.Load() != 0 || ledger.reserves.Load() != 0 || calls.Load() != 0 {
		t.Fatalf("prewarm/metadata consumed admission: %s guard=%d plans=%d reserves=%d calls=%d", metadata.data, len(admitted), planner.plans.Load(), ledger.reserves.Load(), calls.Load())
	}
	for i := 0; i < 2; i++ {
		if err := websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","input":"generate"}`); err != nil {
			t.Fatal(err)
		}
		readSocketCompleted(t, conn)
		out := <-ledger.finalized
		if !out.Charge || !out.Delivered {
			t.Fatalf("generation failed %+v", out)
		}
	}
	if len(admitted) != 2 || planner.plans.Load() != 2 || ledger.reserves.Load() != 2 || calls.Load() != 3 {
		t.Fatalf("retry repeated admission guard=%d plans=%d reserves=%d upstream=%d", len(admitted), planner.plans.Load(), ledger.reserves.Load(), calls.Load())
	}
	if id1, id2 := <-admitted, <-admitted; id1 == id2 {
		t.Fatalf("separate turns reused admission ID %s", id1)
	}
}

func TestRealtimeGuardCountsOneSessionAcrossUpstreamRetry(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		websocket.Server{Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer func() { _ = conn.Close() }()
			_ = websocket.Message.Send(conn, `{"type":"session.created"}`)
			var tail wireFrame
			_ = frameCodec.Receive(conn, &tail)
		})}.ServeHTTP(w, r)
	}))
	defer upstream.Close()
	first := gateway.Target{ChannelID: 1, CredentialID: 10, Provider: "openai", BaseURL: upstream.URL}
	second := first
	second.ChannelID, second.CredentialID = 2, 20
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	h, server, _ := socketFixture(t, []gateway.Target{first, second}, ledger)
	var checks atomic.Int32
	h.cfg.RequestGuard = func(_ context.Context, req *gateway.Request) error {
		checks.Add(1)
		if req.Path != "/v1/realtime" || req.Model != "gpt-realtime" || req.ID == "" {
			t.Error("realtime guard lost session request")
		}
		return nil
	}
	conn := dialSocket(t, server.URL, "/v1/realtime?model=gpt-realtime")
	var frame wireFrame
	if err := frameCodec.Receive(conn, &frame); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	<-ledger.finalized
	if checks.Load() != 1 || ledger.reserves.Load() != 1 || attempts.Load() != 2 {
		t.Fatalf("session retry counted twice guard=%d reserves=%d attempts=%d", checks.Load(), ledger.reserves.Load(), attempts.Load())
	}
}
