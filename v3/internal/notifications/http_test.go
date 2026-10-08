package notifications

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotificationHTTPRejectsUnauthenticatedAndInvalidInput(t *testing.T) {
	s := New(nil, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(r *http.Request) (int64, error) {
		if c, e := r.Cookie("codego_session"); e == nil && c.Value == "test-session" {
			return 7, nil
		}
		return 0, errors.New("unauthorized")
	})
	cases := []struct {
		method, path, cookie, origin string
		want                         int
	}{
		{"GET", "/api/notifications", "", "", 401},
		{"GET", "/api/notifications?page=nope", "test-session", "", 400},
		{"GET", "/api/notifications?page=0", "test-session", "", 400},
		{"GET", "/api/notifications?page_size=101", "test-session", "", 400},
		{"GET", "/api/notifications?unread=1", "test-session", "", 400},
		{"GET", "/api/notifications?category=other", "test-session", "", 400},
		{"POST", "/api/notifications/0/read", "test-session", "", 400},
		{"POST", "/api/notifications/9223372036854775808/read", "test-session", "", 400},
		{"POST", "/api/notifications/read-all?category=other", "test-session", "", 400},
		{"POST", "/api/notifications/read-all", "test-session", "https://evil.test", 403},
		{"GET", "/api/notifications", "test-session", "", 503},
	}
	for _, c := range cases {
		t.Run(c.method+c.path+c.origin, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "http://site.test"+c.path, nil)
			if c.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "codego_session", Value: c.cookie})
			}
			r.Header.Set("Origin", c.origin)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("got %d want %d: %s", w.Code, c.want, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private inbox must not be cached")
			}
		})
	}
}

func TestEventStreamRequiresCookieEvenWithBearer(t *testing.T) {
	s := New(nil, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(r *http.Request) (int64, error) {
		if r.Header.Get("Authorization") == "Bearer accepted-session" {
			return 9, nil
		}
		return 0, errors.New("invalid cookie")
	})
	r := httptest.NewRequest("GET", "http://site.test/api/notifications/events", nil)
	r.Header.Set("Authorization", "Bearer accepted-session")
	r.AddCookie(&http.Cookie{Name: "codego_session", Value: "invalid"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("Bearer overrode invalid stream cookie: %d", w.Code)
	}
}

func TestNotificationReadPayloadValidation(t *testing.T) {
	s := New(nil, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (int64, error) { return 7, nil })
	cases := []struct {
		path, body string
		want       int
	}{
		{"/api/notifications/1/read", "{", 400},
		{"/api/notifications/1/read", "{}", 400},
		{"/api/notifications/1/read", "null", 400},
		{"/api/notifications/1/read", `{"read":"false"}`, 400},
		{"/api/notifications/1/read", `{"read":false,"other":true}`, 400},
		{"/api/notifications/1/read", `{"read":false} {}`, 400},
		{"/api/notifications/1/read", `{"read":false}`, 503},
		{"/api/notifications/1/read", `{"read":true}`, 503},
		{"/api/notifications/read-all", "", 400},
		{"/api/notifications/read-all", `{"through_id":"0"}`, 400},
		{"/api/notifications/read-all", `{"through_id":"+1"}`, 400},
		{"/api/notifications/read-all", `{"through_id":"9223372036854775808"}`, 400},
		{"/api/notifications/read-all", `{"through_id":1}`, 400},
		{"/api/notifications/read-all", `{"through_id":"1","other":1}`, 400},
		{"/api/notifications/read-all?category=market", `{"through_id":"1","category":"review"}`, 400},
		{"/api/notifications/read-all?category=review", `{"through_id":"1","category":"review"}`, 503},
		{"/api/notifications/read-all", `{"through_id":"9007199254740993"}`, 503},
	}
	for _, c := range cases {
		t.Run(c.path+c.body, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://site.test"+c.path, strings.NewReader(c.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, c.want, w.Body.String())
			}
		})
	}
}

func TestEventHubSignalsOnlyRecipientAndCoalesces(t *testing.T) {
	h := newEventHub(nil, nil)
	a, b := make(chan struct{}, 1), make(chan struct{}, 1)
	h.subs[1] = map[chan struct{}]struct{}{a: {}}
	h.subs[2] = map[chan struct{}]struct{}{b: {}}
	h.signal(1)
	h.signal(1)
	if len(a) != 1 || len(b) != 0 {
		t.Fatalf("incorrect event fanout: %d/%d", len(a), len(b))
	}
	h.signal(0)
	if len(a) != 1 || len(b) != 1 {
		t.Fatal("reconnection must invalidate all active recipients")
	}
}

func TestEventHubLimitsTabsAndStopsLastSubscriber(t *testing.T) {
	// A running listener is represented by its cancellation function; no DB
	// access is needed to exercise subscriber admission and lifecycle.
	h := newEventHub(&pgxpool.Pool{}, nil)
	stopped := 0
	h.cancel = func() { stopped++ }
	var stops []func()
	for i := 0; i < 4; i++ {
		_, stop, err := h.subscribe(context.Background(), 7)
		if err != nil {
			t.Fatal(err)
		}
		stops = append(stops, stop)
	}
	if _, _, err := h.subscribe(context.Background(), 7); !errors.Is(err, errTooManyStreams) {
		t.Fatalf("unlimited tabs: %v", err)
	}
	for _, stop := range stops {
		stop()
		stop()
	}
	if stopped != 1 || len(h.subs) != 0 || h.cancel != nil {
		t.Fatalf("listener not reclaimed: stops=%d subscribers=%d", stopped, len(h.subs))
	}
}
