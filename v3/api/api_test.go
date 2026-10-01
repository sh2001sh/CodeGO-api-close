package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeneratedRouterRejectsInvalidParameters(t *testing.T) {
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusNoContent)
	})
	h := DomainHandler(next, func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.WriteHeader(http.StatusBadRequest)
	})
	for _, tc := range []struct {
		url    string
		status int
		calls  int
	}{{"/api/catalog/channels/not-an-id", 400, 0}, {"/api/catalog/channels?page_size=invalid", 400, 0},
		{"/api/channel/test/123?stream=not-a-bool", 400, 0},
		{"/api/community/sellers?page_size=invalid", 400, 0},
		{"/api/billing/history?limit=invalid", 400, 0},
		{"/api/audit/events?event_type=invalid", 400, 0},
		{"/api/audit/requests?page_size=invalid", 400, 0},
		{"/api/audit/requests/public-request/attempts?page_size=invalid", 400, 0},
		{"/api/models/not-an-id", 400, 0},
		{"/api/catalog/models?page_size=invalid", 400, 0},
		{"/api/vendors/not-an-id", 400, 0},
		{"/api/catalog/channels/123", 204, 1},
		{"/api/channel/test/123?stream=true", 204, 2},
		{"/dashboard", 204, 3}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status || nextCalls != tc.calls {
			t.Fatalf("%s status=%d calls=%d; want %d %d", tc.url, w.Code, nextCalls, tc.status, tc.calls)
		}
	}
}

func TestGeneratedRouterPreservesHistoricalIdentifiers(t *testing.T) {
	var requestID, sourceAccount, cursor string
	h := DomainHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID = r.PathValue("request")
		sourceAccount = r.URL.Query().Get("source_account_id")
		cursor = r.URL.Query().Get("before")
		w.WriteHeader(http.StatusNoContent)
	}), func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.WriteHeader(http.StatusBadRequest)
	})
	for _, url := range []string{"/api/audit/requests/original-request:uuid/attempts", "/api/billing/history?source_account_id=key:original-account&before=cursor-_string"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("historical identifier rejected: %s: %d", url, w.Code)
		}
		if strings.HasPrefix(url, "/api/audit/") {
			if requestID != "original-request:uuid" {
				t.Fatalf("request ID changed: %q", requestID)
			}
		} else if sourceAccount != "key:original-account" || cursor != "cursor-_string" {
			t.Fatalf("historical account/cursor changed: %q %q", sourceAccount, cursor)
		}
	}
}

func TestGeneratedRouterPreservesCommunityPublicID(t *testing.T) {
	var id string
	h := DomainHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = r.PathValue("id")
		w.WriteHeader(http.StatusNoContent)
	}), func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.WriteHeader(http.StatusBadRequest)
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/community/channels/public-channel-id/rating", nil))
	if w.Code != http.StatusNoContent || id != "public-channel-id" {
		t.Fatalf("community public ID changed: status=%d id=%q", w.Code, id)
	}
}

func TestPublishedSpecIsEmbedded(t *testing.T) {
	w := httptest.NewRecorder()
	Specification(w, httptest.NewRequest("GET", "/api/openapi.json", nil))
	var spec struct {
		Version string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Version != "3.0.3" || len(spec.Paths) < 90 || len(spec.Paths["/api/user/login"]) == 0 {
		t.Fatalf("incomplete embedded contract: %s %d", spec.Version, len(spec.Paths))
	}
}
