package community

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionRatingRejectsSuppliedIdentityAndInvalidStars(t *testing.T) {
	s := New(nil, Config{})
	mux := http.NewServeMux()
	lookups := 0
	s.RegisterSessionRoutes(mux, func(*http.Request) (int64, error) { return 17, nil }, func(context.Context, int64) (string, error) {
		lookups++
		return "ABC234", nil
	})
	for _, body := range []string{`{"stars":5,"viewer_sub":"DEF567"}`, `{"stars":0}`, `{"stars":1.5}`, `{"stars":5}{}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/community/channels/channel-1/rating", strings.NewReader(body)))
		if w.Code != 400 || lookups != 0 {
			t.Fatalf("untrusted rating accepted: %d %s lookups=%d", w.Code, w.Body, lookups)
		}
	}
}

func TestSessionRoutesNeverAcceptServiceSecretAsSession(t *testing.T) {
	s := New(nil, Config{ServiceSecret: strings.Repeat("s", 32)})
	mux := http.NewServeMux()
	s.RegisterSessionRoutes(mux, nil, nil)
	for _, route := range []struct{ method, path string }{{"GET", "/api/community/sellers"}, {"POST", "/api/community/channels/channel-1/rating"}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{"stars":5}`))
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 32))
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("service secret granted browser access: %d", w.Code)
		}
	}
}
