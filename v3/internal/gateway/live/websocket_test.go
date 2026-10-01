package live

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

type livePlan struct{ targets []gateway.Target }

func (p livePlan) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return append([]gateway.Target(nil), p.targets...), nil
}
func (livePlan) Report(gateway.Target, gateway.AttemptResult) {}

type liveLedger struct {
	reserves  atomic.Int32
	finalized chan gateway.Outcome
	err       error
}

func (l *liveLedger) Reserve(context.Context, *gateway.Request) error {
	l.reserves.Add(1)
	return l.err
}
func (l *liveLedger) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	l.finalized <- out
	return nil
}

type liveRepo struct {
	mu    sync.Mutex
	items map[string]Locator
	err   error
}

func (r *liveRepo) Put(_ context.Context, l Locator, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.items[l.ID] = l
	return nil
}
func (r *liveRepo) Get(_ context.Context, id string, user, key int64) (Locator, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.items[id]
	if !ok || l.UserID != user || l.KeyID != key {
		return Locator{}, ErrNotFound
	}
	return l, nil
}
func (r *liveRepo) Delete(_ context.Context, id string, user, key int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.items[id]
	if l.UserID == user && l.KeyID == key {
		delete(r.items, id)
	}
	return nil
}

func socketFixture(t *testing.T, targets []gateway.Target, ledger *liveLedger) (*Handler, *httptest.Server, *liveRepo) {
	t.Helper()
	repo := &liveRepo{items: map[string]Locator{}}
	h, err := New(Config{Auth: backgroundAuth{}, Planner: livePlan{targets: targets}, Settler: ledger, Repository: repo, Providers: map[string]gateway.Provider{"openai": responses.Provider{}}, Resolve: func(_ context.Context, ch, cr int64) (gateway.Target, error) {
		for _, target := range targets {
			if target.ChannelID == ch && target.CredentialID == cr {
				return target, nil
			}
		}
		return gateway.Target{}, ErrNotFound
	}, SessionTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return h, server, repo
}

func dialSocket(t *testing.T, server, path string) *websocket.Conn {
	t.Helper()
	cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(server, "http")+path, server)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Header.Set("Authorization", "Bearer owner")
	conn, err := cfg.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readSocketCompleted(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	for i := 0; i < 10; i++ {
		var f wireFrame
		if err := frameCodec.Receive(conn, &f); err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(f.data, "type").Str == "response.completed" {
			return f.data
		}
		if gjson.GetBytes(f.data, "type").Str == "error" {
			t.Fatalf("unexpected socket error %s", f.data)
		}
	}
	t.Fatal("no completion event")
	return nil
}

func testResponseSSE(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":%q}}\n\n", id)
	_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":9,\"output_tokens\":2}}}\n\n", id)
}

func TestResponsesWebSocketTurnsAreBilledAndHistoryIsMerged(t *testing.T) {
	requests := make(chan []byte, 2)
	var n atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("wrong upstream key")
		}
		body, _ := io.ReadAll(r.Body)
		requests <- body
		testResponseSSE(w, fmt.Sprintf("resp_%d", n.Add(1)))
	}))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 4)}
	_, server, repo := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL, Secret: "upstream-secret"}}, ledger)
	conn := dialSocket(t, server.URL, "/responses")
	_ = frameCodec.Send(conn, wireFrame{kind: websocket.TextFrame, data: []byte(`{"type":"response.create","model":"gpt-test","input":"one"}`)})
	readSocketCompleted(t, conn)
	first := <-ledger.finalized
	if !first.Charge || first.Usage.PromptTokens != 9 || first.Usage.CompletionTokens != 2 || first.Terminal != gateway.TerminalCompleted {
		t.Fatalf("first outcome %+v", first)
	}
	_ = frameCodec.Send(conn, wireFrame{kind: websocket.BinaryFrame, data: []byte(`{"type":"response.append","input":"two"}`)})
	readSocketCompleted(t, conn)
	second := <-ledger.finalized
	if !second.Charge {
		t.Fatal("second turn wasn't billed")
	}
	if ledger.reserves.Load() != 2 {
		t.Fatalf("reserve calls=%d", ledger.reserves.Load())
	}
	<-requests
	next := <-requests
	if gjson.GetBytes(next, "model").Str != "gpt-test" || gjson.GetBytes(next, "input.#").Int() != 3 {
		t.Fatalf("history lost: %s", next)
	}
	loc, err := repo.Get(context.Background(), "resp_2", 1, 11)
	if err != nil || loc.ChannelID != 1 || loc.CredentialID != 2 {
		t.Fatalf("locator=%+v %v", loc, err)
	}
}

func TestResponsesWebSocketRetriesBeforeOutputOnly(t *testing.T) {
	var brokenCalls, goodCalls atomic.Int32
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { brokenCalls.Add(1); w.WriteHeader(503) }))
	defer broken.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { goodCalls.Add(1); testResponseSSE(w, "resp_ok") }))
	defer good.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 3)}
	_, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 1, Provider: "openai", BaseURL: broken.URL}, {ChannelID: 2, CredentialID: 2, Provider: "openai", BaseURL: good.URL}}, ledger)
	conn := dialSocket(t, server.URL, "/v1/responses")
	_ = websocket.Message.Send(conn, `{"type":"response.create","model":"gpt","input":"one"}`)
	readSocketCompleted(t, conn)
	out := <-ledger.finalized
	if brokenCalls.Load() != 1 || goodCalls.Load() != 1 || out.Target.ChannelID != 2 {
		t.Fatalf("retry not executed: %d %d %+v", brokenCalls.Load(), goodCalls.Load(), out)
	}
}

func TestResponsesWebSocketNeverRetriesAfterOutput(t *testing.T) {
	var calls atomic.Int32
	partial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"failed\"}}}\n\n")
	}))
	defer partial.Close()
	unused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); testResponseSSE(w, "resp_other") }))
	defer unused.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2)}
	_, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 1, Provider: "openai", BaseURL: partial.URL}, {ChannelID: 2, CredentialID: 2, Provider: "openai", BaseURL: unused.URL}}, ledger)
	conn := dialSocket(t, server.URL, "/v1/responses")
	_ = websocket.Message.Send(conn, `{"type":"response.create","model":"gpt","input":"one"}`)
	var frame wireFrame
	if err := frameCodec.Receive(conn, &frame); err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(frame.data, "delta").Str != "partial" {
		t.Fatalf("data=%s", frame.data)
	}
	if err := frameCodec.Receive(conn, &frame); err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(frame.data, "type").Str != "error" {
		t.Fatalf("failure=%s", frame.data)
	}
	out := <-ledger.finalized
	if !out.Charge || out.Terminal != gateway.TerminalUpstreamErrorAfterOutput || calls.Load() != 0 {
		t.Fatalf("outcome=%+v retries=%d", out, calls.Load())
	}
}

func TestResponsesWebSocketTimeoutRefundsBeforeOutput(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2)}
	h, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 1, Provider: "openai", BaseURL: upstream.URL}}, ledger)
	h.cfg.SessionTimeout = 60 * time.Millisecond
	conn := dialSocket(t, server.URL, "/v1/responses")
	_ = websocket.Message.Send(conn, `{"type":"response.create","model":"gpt","input":"one"}`)
	select {
	case out := <-ledger.finalized:
		if out.Charge || out.Terminal != gateway.TerminalTimeout {
			t.Fatalf("outcome=%+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout didn't finalize")
	}
}

func TestResponsesWebSocketBillingFailureAndInvalidAppend(t *testing.T) {
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 2), err: gateway.ErrInsufficientCredits}
	_, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 1, Provider: "openai", BaseURL: "http://invalid"}}, ledger)
	conn := dialSocket(t, server.URL, "/backend-api/codex/responses")
	for _, tc := range []struct {
		body, code string
		status     int
	}{{`{"type":"response.append","input":"one"}`, "previous_response_not_found", 400}, {`{"type":"response.create","model":"gpt","input":"one"}`, "billing_unavailable", 402}} {
		_ = websocket.Message.Send(conn, tc.body)
		var f wireFrame
		if err := frameCodec.Receive(conn, &f); err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(f.data, "error.code").Str != tc.code || gjson.GetBytes(f.data, "status").Int() != int64(tc.status) {
			t.Fatalf("error=%s", f.data)
		}
	}
	if ledger.reserves.Load() != 1 {
		t.Fatalf("unexpected reserve=%d", ledger.reserves.Load())
	}
}
