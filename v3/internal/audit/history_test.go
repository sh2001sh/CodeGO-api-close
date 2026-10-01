package audit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHistoryHTTPRejectsUnauthorizedOwnershipAndMalformedCursors(t *testing.T) {
	auth := func(*http.Request) (Principal, error) { return Principal{UserID: 7, KeyID: 11, Admin: true}, nil }
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/api/audit/events?user_id=8", 403},
		{"/api/audit/events?key_id=12", 403},
		{"/api/audit/events?event_type=7", 400},
		{"/api/audit/events?event_type=-1", 400},
		{"/api/audit/events?cursor=broken", 400},
		{"/api/audit/requests?user_id=8", 403},
		{"/api/audit/requests?cursor=broken", 400},
		{"/api/audit/requests/r/attempts?cursor=broken", 400},
		{"/api/audit/requests/r/attempts?page_size=201", 400},
	} {
		w := httptest.NewRecorder()
		New(nil, Config{Authenticate: auth}).Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s got %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/audit/events", "/api/audit/events/export", "/api/audit/requests", "/api/audit/requests/r/attempts"} {
		w := httptest.NewRecorder()
		New(nil, Config{}).Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("anonymous access %s: %d", path, w.Code)
		}
	}
}

func TestHistoricalStringCursorsPreserveIDsAndRejectInvalidBounds(t *testing.T) {
	a := requestCursor{At: time.Date(2026, 9, 30, 12, 0, 0, 123, time.UTC), ID: "original:req/string"}
	got, err := decodeRequestCursor(encodeRequestCursor(a))
	if err != nil || got != a {
		t.Fatalf("request cursor %v %v", got, err)
	}
	b := attemptCursor{Number: 0, ID: "first-attempt"}
	gotAttempt, err := decodeAttemptCursor(encodeAttemptCursor(b))
	if err != nil || gotAttempt != b {
		t.Fatalf("attempt cursor %v %v", gotAttempt, err)
	}
	for _, value := range []string{"broken", strings.Repeat("a", 2049), encodeRequestCursor(requestCursor{ID: "missing date"})} {
		if _, err := decodeRequestCursor(value); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid request cursor accepted: %q", value[:min(30, len(value))])
		}
	}
	if _, err := decodeAttemptCursor(encodeAttemptCursor(attemptCursor{Number: -1, ID: "bad"})); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative attempt cursor accepted")
	}
}
