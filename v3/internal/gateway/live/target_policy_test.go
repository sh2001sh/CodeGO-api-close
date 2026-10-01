package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestBackgroundTargetPolicyRejectsBeforeReserveAndQueuedWorkerRefunds(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "creation", true: "restored-worker"}[restored], func(t *testing.T) {
			var upstreamCalls atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls.Add(1) }))
			defer upstream.Close()
			h, _, wrapped, repo, billing := backgroundJobsFixture(t, upstream.URL, "openai")
			deny := func(req *gateway.Request, target gateway.Target) error {
				if target.ChannelID != 7 || target.CredentialID != 9 || gjson.GetBytes(req.Body, "input").Str != "blocked" {
					t.Errorf("target policy missed frozen source/selected route: %+v %s", target, req.Body)
				}
				return errors.New("prompt denied")
			}
			body := `{"model":"gpt-test","background":true,"input":"blocked"}`
			if !restored {
				h.cfg.TargetPolicy = deny
				r := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer owner")
				w := httptest.NewRecorder()
				wrapped.ServeHTTP(w, r)
				if w.Code != 403 || billing.reserves != 0 || upstreamCalls.Load() != 0 {
					t.Fatalf("denied creation reserved/submitted status=%d reserves=%d upstream=%d", w.Code, billing.reserves, upstreamCalls.Load())
				}
				return
			}
			id := createBackgroundForTest(t, wrapped, "/responses", body)
			h.cfg.TargetPolicy = deny
			if err := h.Reconcile(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			job, err := repo.GetOwned(context.Background(), id, 1, 11)
			out, count, _ := billing.result(id)
			if err != nil || job.Status != "failed" || !job.Billed || out.Charge || count != 1 || upstreamCalls.Load() != 0 {
				t.Fatalf("restored denied job not refunded without upstream: job=%+v outcome=%+v calls=%d", job, out, upstreamCalls.Load())
			}
		})
	}
}

func TestResponsesTargetPolicyRejectsFrozenPromptBeforeReserve(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	target := gateway.Target{ChannelID: 3, CredentialID: 7, Provider: "openai", BaseURL: upstream.URL,
		Settings: map[string]any{"pass_through_body_enabled": true}, ParamOverride: map[string]any{"input": "overridden"}}
	h, server, _ := socketFixture(t, []gateway.Target{target}, ledger)
	h.cfg.TargetPolicy = func(req *gateway.Request, target gateway.Target) error {
		if target.ChannelID != 3 || target.CredentialID != 7 || gjson.GetBytes(req.Body, "input").Str != "blocked" {
			t.Errorf("policy saw converted prompt or wrong target: %+v %s", target, req.Body)
		}
		return errors.New("prompt denied")
	}
	conn := dialSocket(t, server.URL, "/responses")
	_ = websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","input":"blocked"}`)
	var message wireFrame
	if err := frameCodec.Receive(conn, &message); err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(message.data, "status").Int() != 403 || ledger.reserves.Load() != 0 || calls.Load() != 0 {
		t.Fatalf("denied socket reserved/submitted message=%s reserves=%d calls=%d", message.data, ledger.reserves.Load(), calls.Load())
	}
}

type targetPolicyPlanner struct {
	livePlan
	reports atomic.Int64
}

func (p *targetPolicyPlanner) Report(target gateway.Target, _ gateway.AttemptResult) {
	if target.ChannelID == 2 {
		p.reports.Add(100)
		return
	}
	p.reports.Add(1)
}

func TestResponsesRetryPolicyDenialDoesNotSubmitOrCoolCredential(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":{"message":"retry"}}`)
	}))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	first := gateway.Target{ChannelID: 1, CredentialID: 10, Provider: "openai", BaseURL: upstream.URL}
	second := first
	second.ChannelID, second.CredentialID = 2, 20
	h, server, _ := socketFixture(t, []gateway.Target{first, second}, ledger)
	planner := &targetPolicyPlanner{livePlan: livePlan{targets: []gateway.Target{first, second}}}
	h.cfg.Planner = planner
	h.cfg.TargetPolicy = func(_ *gateway.Request, target gateway.Target) error {
		if target.ChannelID == 2 {
			return errors.New("retry route prompt denied")
		}
		return nil
	}
	conn := dialSocket(t, server.URL, "/responses")
	_ = websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","input":"test"}`)
	var message wireFrame
	if err := frameCodec.Receive(conn, &message); err != nil {
		t.Fatal(err)
	}
	out := <-ledger.finalized
	if gjson.GetBytes(message.data, "status").Int() != 403 || out.Charge || calls.Load() != 1 || planner.reports.Load() != 1 {
		t.Fatalf("denied retry submitted/cooled/charged message=%s out=%+v calls=%d reports=%d", message.data, out, calls.Load(), planner.reports.Load())
	}
}

func TestBackgroundUsesSharedSensitiveWordPolicyAndTypedErrors(t *testing.T) {
	for _, mode := range []string{"blocked", "invalid-config", "channel-opt-out"} {
		t.Run(mode, func(t *testing.T) {
			h, _, wrapped, _, billing := backgroundJobsFixture(t, "https://unused.invalid", "openai")
			settings := map[string]json.RawMessage{"SensitiveWords": json.RawMessage(`["contains:blocked"]`)}
			if mode == "invalid-config" {
				settings["SensitiveWords"] = json.RawMessage(`invalid`)
			}
			h.cfg.TargetPolicy = gateway.NewSensitiveWordPolicy(func() map[string]json.RawMessage { return settings }, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if mode == "channel-opt-out" {
				target, _ := h.cfg.Resolve(context.Background(), 7, 9)
				target.Settings = map[string]any{"sensitive_word_interception_enabled": false}
				h.cfg.Planner = backgroundJobsPlanner{target: target}
			}
			r := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(`{"model":"gpt-test","background":true,"input":"blocked"}`))
			r.Header.Set("Authorization", "Bearer owner")
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, r)
			wantStatus, wantCode, reserves := 403, "sensitive_words_detected", 0
			switch mode {
			case "invalid-config":
				wantStatus, wantCode = 503, "target_policy_unavailable"
			case "channel-opt-out":
				wantStatus, wantCode, reserves = 200, "", 1
			}
			if w.Code != wantStatus || gjson.GetBytes(w.Body.Bytes(), "error.code").Str != wantCode || billing.reserves != reserves {
				t.Fatalf("shared policy mode=%s status=%d body=%s reserves=%d", mode, w.Code, w.Body.String(), billing.reserves)
			}
		})
	}
}
