package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestAuthorizationAndOrigin(t *testing.T) {
	if _, err := New(Config{}, nil); err == nil {
		t.Fatal("missing authentication accepted")
	}
	for _, tc := range []struct {
		name   string
		user   Principal
		origin string
		status int
	}{{"anonymous", Principal{}, "https://codego.test", 401},
		{"regular-user", Principal{UserID: 1}, "https://codego.test", 403},
		{"cross-site", Principal{UserID: 1, Admin: true}, "https://other.test", 403},
		{"scheme-downgrade", Principal{UserID: 1, Admin: true}, "http://codego.test", 403},
		{"admin", Principal{UserID: 1, Admin: true}, "https://codego.test", 204}} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New(Config{PublicURL: "https://codego.test", Authenticate: func(*http.Request) (Principal, error) {
				return tc.user, nil
			}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			s.Mux().Handle("POST /api/private", s.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(204)
			})))
			req := httptest.NewRequest("POST", "https://codego.test/api/private", nil)
			req.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d; want %d", w.Code, tc.status)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("API response may be cached")
			}
		})
	}
}

func TestReadinessFailure(t *testing.T) {
	s, _ := New(Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{}, nil },
		Ready: func(context.Context) error { return errors.New("database disconnected") }}, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "database disconnected") {
		t.Fatalf("readiness response leaked cause or passed: %d %s", w.Code, w.Body)
	}
}

func TestAPIMethodFailuresWithApplicationFallback(t *testing.T) {
	s, _ := New(Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{}, nil }}, nil)
	s.Mux().Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"POST", "/api/status", 405}, {"PATCH", "/api/missing", 404}, {"GET", "/api", 404}, {"GET", "/dashboard", 200}} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d want %d", tc.method, tc.path, w.Code, tc.status)
		}
		if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("allowed methods: %q", w.Header().Get("Allow"))
		}
	}
}

func TestOIDCProtocolOriginPolicy(t *testing.T) {
	s, _ := New(Config{PublicURL: "https://codego.test", Authenticate: func(*http.Request) (Principal, error) { return Principal{}, nil }}, nil)
	for _, path := range []string{"/api/oidc/token", "/api/oidc/userinfo", "/api/private"} {
		s.Mux().Handle("POST "+path, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("Origin", "https://community.test")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		s.Handler().ServeHTTP(w, r)
		want := 401
		if path == "/api/private" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("%s: %d want %d", path, w.Code, want)
		}
	}
}

func TestStaticCompressionAndAPIMiss(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "entry", "assets/app.js": "source",
		"assets/app.js.br": "brotli", "assets/app.js.gz": "gzip", "static/app.js": "static"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path, accept, body, encoding string
		status                       int
	}{
		{"/dashboard", "", "entry", "", 200},
		{"/assets/app.js", "br, gzip", "brotli", "br", 200},
		{"/assets/app.js", "br;q=0.00, gzip", "gzip", "gzip", 200},
		{"/static/app.js", "", "static", "", 200},
		{"/assets/missing.js", "", "", "", 404},
		{"/api/missing", "", "", "", 404},
		{"/.well-known/missing", "", "", "", 404},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Accept-Encoding", tc.accept)
		Static(root).ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Content-Encoding") != tc.encoding {
			t.Fatalf("%s: %d %q", tc.path, w.Code, w.Header().Get("Content-Encoding"))
		}
		if tc.body != "" && w.Body.String() != tc.body {
			t.Fatalf("%s body = %s", tc.path, w.Body)
		}
		if tc.status == 200 && (strings.HasPrefix(tc.path, "/assets/") || strings.HasPrefix(tc.path, "/static/")) && !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
			t.Fatal("hashed assets not immutable")
		}
	}
}

type fakePoster struct {
	entry billing.Entry
	calls int
	err   error
}

func (p *fakePoster) Post(_ context.Context, entry billing.Entry) (billing.PostResult, error) {
	p.entry, p.calls = entry, p.calls+1
	return billing.PostResult{EntryID: 8, Balance: 100, Version: 1}, p.err
}

func TestAdjustmentBoundaryAndConflict(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		err           error
		status, calls int
	}{
		{"zero", `{"account_id":1,"amount_micro":0,"operation_id":"x","reason":"correction"}`, nil, 400, 0},
		{"missing-id", `{"account_id":1,"amount_micro":1,"reason":"correction"}`, nil, 400, 0},
		{"float-money", `{"account_id":1,"amount_micro":1.5,"operation_id":"x","reason":"correction"}`, nil, 400, 0},
		{"trailing-json", `{"account_id":1,"amount_micro":1,"operation_id":"x","reason":"correction"}{}`, nil, 400, 0},
		{"conflicting-id", `{"account_id":1,"amount_micro":1,"operation_id":"x","reason":"correction"}`, billing.ErrPostConflict, 409, 1},
		{"accepted", `{"account_id":1,"amount_micro":1,"operation_id":"x","reason":"correction"}`, nil, 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := New(Config{Authenticate: func(*http.Request) (Principal, error) {
				return Principal{UserID: 1, Admin: true}, nil
			}}, nil)
			p := &fakePoster{err: tc.err}
			s.RegisterBilling(nil, nil, p)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/billing/adjustments", strings.NewReader(tc.payload)))
			if w.Code != tc.status || p.calls != tc.calls {
				t.Fatalf("status=%d calls=%d; want status=%d calls=%d", w.Code, p.calls, tc.status, tc.calls)
			}
			if p.calls > 0 && p.entry.OperationID != "admin-adjustment:x" {
				t.Fatal("operation not namespaced")
			}
		})
	}
}
