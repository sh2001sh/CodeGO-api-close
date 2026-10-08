package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/bench/mockupstream"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

type summaryCapture struct{ records []audit.RequestRecord }

func (s *summaryCapture) RecordRequest(req *gateway.Request, out gateway.Outcome, settled bool) {
	s.records = append(s.records, audit.ProjectRequestRecord(req, out, settled))
}

func (*summaryCapture) Close() {}

type summaryAuth struct{}

func (summaryAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	return gateway.Principal{UserID: 1, KeyID: 1, Group: "default"}, nil
}

type summaryPlanner []gateway.Target

func (p summaryPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return p, nil
}
func (summaryPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type summarySettler struct{}

func (summarySettler) Reserve(context.Context, *gateway.Request) error                   { return nil }
func (summarySettler) Finalize(context.Context, *gateway.Request, gateway.Outcome) error { return nil }

type disconnectedWriter struct{ *httptest.ResponseRecorder }

func (disconnectedWriter) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

func summaryHTTPGateway(t *testing.T, rec gateway.RequestRecorder, scenarios ...string) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(mockupstream.Handler())
	t.Cleanup(upstream.Close)
	var routes summaryPlanner
	for i, scenario := range scenarios {
		routes = append(routes, gateway.Target{ChannelID: int64(i + 1), Provider: openai.ID, Group: "default", Secret: "not-recorded", BaseURL: upstream.URL + "/m/" + scenario})
	}
	g, err := gateway.New(gateway.Deps{Config: gateway.Config{Heartbeat: time.Hour}, Authorizer: summaryAuth{}, Planner: routes, Settler: summarySettler{}, Requests: rec, Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	g.Register(mux)
	return mux
}

func summaryHTTPRequest(handler http.Handler, w http.ResponseWriter, stream bool) {
	body := `{"model":"contract-model","messages":[{"role":"user","content":"secret prompt"}],"stream":false}`
	if stream {
		body = strings.Replace(body, "false", "true", 1)
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer sk-fixture")
	handler.ServeHTTP(w, r)
}

func TestActualGatewayHTTPProducesOneLogicalTerminalSummary(t *testing.T) {
	for _, tc := range []struct {
		name, status    string
		scenarios       []string
		attempts, code  int64
		cancel, counted bool
	}{
		{"success", "success", []string{"complete/c3/i0/t0"}, 1, 200, false, true},
		{"upstream_failure", "failed", []string{"error_before/t0"}, 1, 429, false, true},
		{"retry", "success", []string{"error_before/t0", "complete/c3/i0/t0"}, 2, 200, false, true},
		{"empty", "failed", []string{"empty/t0"}, 1, 502, false, true},
		{"client_cancel", "cancelled", []string{"complete/c3/i0/t0"}, 1, 499, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &summaryCapture{}
			h := summaryHTTPGateway(t, s, tc.scenarios...)
			w := httptest.NewRecorder()
			if tc.cancel {
				summaryHTTPRequest(h, disconnectedWriter{w}, true)
			} else {
				summaryHTTPRequest(h, w, true)
			}
			s.Close()
			records := s.records
			if len(records) != 1 || records[0].Status != tc.status || records[0].Attempts != tc.attempts || records[0].Retries != tc.attempts-1 || records[0].StatusCode != tc.code || records[0].Counted != tc.counted {
				t.Fatalf("actual HTTP terminal summary differs: %+v", records)
			}
		})
	}
}
