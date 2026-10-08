package channelmarket

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOwnerAnalyticsRejectsInvalidFiltersAndUnauthenticatedRequests(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s := New(nil, nil, nil, Config{Now: func() time.Time { return now }}, nil)
	for _, query := range []string{
		"from=broken", "to=broken", "from=2026-10-07T12:00:00Z&to=2026-10-07T12:00:00Z",
		"from=2024-01-01T00:00:00Z", "channel_id=-1", "channel_id=01", "channel_id=1%20OR%201=1",
		"channel_id=9223372036854775808", "model=bad%0Amodel",
	} {
		if _, err := s.ownerAnalyticsFilter(httptest.NewRequest(http.MethodGet, "/?"+query, nil)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid analytics filter accepted %q: %v", query, err)
		}
	}
	f, err := s.ownerAnalyticsFilter(httptest.NewRequest(http.MethodGet, "/?to=2026-10-06T08:00:00%2B08:00", nil))
	if err != nil || f.To.Format(time.RFC3339) != "2026-10-06T00:00:00Z" || f.To.Sub(f.From) != 7*24*time.Hour {
		t.Fatalf("timezone/end-only filter: %+v %v", f, err)
	}
	mux := http.NewServeMux()
	s.RegisterOwnerAnalyticsHTTP(mux, nil)
	for _, route := range []string{"/api/marketplace/channels/mine/analytics", "/api/marketplace/channels/mine/analytics/export"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, route, nil))
		if w.Code != http.StatusForbidden || w.Header().Get("Content-Disposition") != "" {
			t.Fatalf("unauthenticated analytics accessible: %s %d", route, w.Code)
		}
	}
}

func TestOwnerReportsPreserveUnfilteredHistoryAndRejectInvalidRange(t *testing.T) {
	f, err := ownerReportFilter(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil || !f.From.IsZero() || !f.To.IsZero() {
		t.Fatalf("legacy report acquired an implicit time limit: %+v %v", f, err)
	}
	for _, query := range []string{"from=bad", "to=bad", "from=2026-10-07T00:00:00Z&to=2026-10-06T00:00:00Z", "channel_id=bad"} {
		if _, err = ownerReportFilter(httptest.NewRequest(http.MethodGet, "/?"+query, nil)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid report filter accepted %s: %v", query, err)
		}
	}
}
