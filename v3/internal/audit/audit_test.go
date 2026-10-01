package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScopeProtectsUserAndKeyOwnership(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Principal
		q    Query
		want error
	}{
		{"anonymous", Principal{}, Query{}, ErrForbidden},
		{"other user", Principal{UserID: 1}, Query{UserID: 2}, ErrForbidden},
		{"other key", Principal{UserID: 1, KeyID: 3}, Query{KeyID: 4}, ErrForbidden},
		{"key cannot use admin", Principal{UserID: 1, KeyID: 3, Admin: true}, Query{UserID: 2}, ErrForbidden},
		{"invalid cursor", Principal{UserID: 1}, Query{Cursor: "broken"}, ErrInvalid},
		{"oversized page", Principal{UserID: 1}, Query{Limit: 201}, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scope(tc.p, tc.q)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	q, err := scope(Principal{UserID: 1}, Query{})
	if err != nil || q.UserID != 1 || q.Limit != 50 {
		t.Fatalf("scope: %+v %v", q, err)
	}
	q, err = scope(Principal{UserID: 1, Admin: true}, Query{UserID: 2})
	if err != nil || q.UserID != 2 {
		t.Fatalf("admin scope: %+v %v", q, err)
	}
}

func TestHTTPDeniesMissingAuthAndInvalidFiltersBeforeStorage(t *testing.T) {
	for _, tc := range []struct {
		cfg    Config
		path   string
		status int
	}{
		{Config{}, "/api/log/self", 401},
		{Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }}, "/api/log/", 403},
		{Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }}, "/api/log/token", 403},
		{Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }}, "/api/log/self?user_id=2", 403},
		{Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }}, "/api/log/self?page_size=-1", 400},
		{Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }}, "/api/log/self?cursor=broken", 400},
		{Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 1}, nil }}, "/api/audit/samples/sample", 403},
	} {
		r := httptest.NewRecorder()
		New(nil, tc.cfg).Handler().ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
		if r.Code != tc.status {
			t.Fatalf("%s: got %d body %s", tc.path, r.Code, r.Body.String())
		}
	}
}

func TestCursorRoundTripAndTimeBoundaries(t *testing.T) {
	u := Usage{ID: 77, CreatedAt: time.Date(2026, 9, 30, 1, 2, 3, 12345, time.UTC)}
	c, err := decodeCursor(encodeCursor(u))
	if err != nil || c.ID != u.ID || !c.At.Equal(u.CreatedAt) {
		t.Fatalf("cursor %+v %v", c, err)
	}
	if _, err := scope(Principal{UserID: 1}, Query{From: u.CreatedAt, To: u.CreatedAt}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid interval accepted: %v", err)
	}
}

func TestSamplingRedactsCredentialsAndRejectsInvalidBodies(t *testing.T) {
	service := New(nil, Config{SampleRatePPM: 1_000_000})
	if !service.ShouldSample("request") || New(nil, Config{}).ShouldSample("request") {
		t.Fatal("sampling endpoints")
	}
	raw := json.RawMessage(`{"headers":{"Authorization":"Bearer secret","cookie":"secret","X-Api-Key":"secret"},"nested":[{"refresh_token":"secret"}],"amount":9007199254740993}`)
	out, err := redact(raw, 4096)
	if err != nil || strings.Contains(string(out), "secret") || !strings.Contains(string(out), "9007199254740993") {
		t.Fatalf("redact %s %v", out, err)
	}
	for _, raw := range []string{`{} {}`, `{} garbage`, `{"x":`, strings.Repeat("x", 5000)} {
		if _, err := redact(json.RawMessage(raw), 4096); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid body %q", raw[:min(len(raw), 30)])
		}
	}
	if err := service.RecordSample(context.Background(), Sample{RequestID: "request"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid sample: %v", err)
	}
	for _, input := range []string{"=1+1", "+cmd", "-formula", "@sum", "\t=1"} {
		if safeCSV(input) != "'"+input {
			t.Fatalf("formula escaped incorrectly: %q", safeCSV(input))
		}
	}
}
