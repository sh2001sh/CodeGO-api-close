package live

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBackgroundStreamAndResumeCursor(t *testing.T) {
	events := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\",\"sequence_number\":43}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\"}}\n\n"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("starting_after") != "42" || r.URL.Query().Get("stream") != "true" {
			t.Error("stream resume query was not forwarded")
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, events)
		w.(http.Flusher).Flush()
	}))
	defer up.Close()
	_, mux, _, limits := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
	w := backgroundCall(mux, http.MethodGet, "/responses/resp_123?stream=1&starting_after=42", "owner")
	if w.Code != 200 || w.Body.String() != events || !w.Flushed || w.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream status=%d flushed=%v body=%q", w.Code, w.Flushed, w.Body.String())
	}
	if limits.releases != 1 {
		t.Fatal("stream lease leaked")
	}
}

func TestBackgroundQueryFailures(t *testing.T) {
	var called atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Add(1) }))
	defer up.Close()
	_, mux, _, _ := backgroundFixture(t, gateway.Target{BaseURL: up.URL})
	for _, query := range []string{"stream=garbage", "stream=true&stream=false", "starting_after=garbage", "starting_after=-2", "starting_after=1&starting_after=2", "starting_after=9223372036854775808", "x=%zz"} {
		w := backgroundCall(mux, "GET", "/v1/responses/resp_123?"+query, "owner")
		if w.Code != 400 {
			t.Errorf("query %q: status %d", query, w.Code)
		}
	}
	if called.Load() != 0 {
		t.Fatal("invalid query reached upstream")
	}
}

func TestBackgroundFailuresAndRedirectProtection(t *testing.T) {
	var leaked atomic.Int64
	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
	defer evil.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "redirect":
			w.Header().Set("Location", evil.URL)
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "oversized":
			_, _ = io.WriteString(w, strings.Repeat("x", 20))
		case "error":
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limit"}}`)
		}
	}))
	defer up.Close()
	for _, testCase := range []string{"redirect", "oversized", "error"} {
		h, mux, _, limits := backgroundFixture(t, gateway.Target{BaseURL: up.URL + "?case=" + testCase})
		if testCase == "oversized" {
			h.cfg.MaxBodyBytes = 10
		}
		w := backgroundCall(mux, "GET", "/v1/responses/resp_123", "owner")
		want := 502
		if testCase == "error" {
			want = 429
			if w.Header().Get("Retry-After") != "3" || !strings.Contains(w.Body.String(), "rate_limit") {
				t.Error("upstream error was not preserved")
			}
		}
		if w.Code != want || limits.releases != 1 {
			t.Errorf("case=%s status=%d limits=%+v", testCase, w.Code, limits)
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("upstream redirect was followed")
	}
}
