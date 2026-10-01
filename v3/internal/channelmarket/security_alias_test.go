package channelmarket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSecurityAliasesKeepStringIDsEnvelopeFiltersAndAdminGate(t *testing.T) {
	var calls atomic.Int64
	var admin bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == http.MethodPatch && r.PathValue("id") != "original-string-id" {
			t.Errorf("legacy string ID was lost: %s", r.PathValue("id"))
		}
		if r.Method == http.MethodGet && (r.URL.Query().Get("marketplace_channel") != "legacy-channel" || r.URL.Query().Get("page_size") != "20") {
			t.Error("legacy filter/defaults lost")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"original-string-id"}`))
	})
	s := New(nil, nil, nil, Config{SecurityAuditHandler: handler}, nil)
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (Actor, error) { return Actor{UserID: 1, Admin: admin}, nil })
	for _, path := range []string{"/api/marketplace/security-audit/events?channel_id=legacy-channel", "/api/marketplace/admin/security-audit/events?channel_id=legacy-channel"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if strings.Contains(path, "/admin/") {
			if w.Code != 403 || calls.Load() != 1 {
				t.Fatal("non-admin reached privileged handler")
			}
			continue
		}
		var body struct {
			Success bool `json:"success"`
			Data    struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || !body.Success || body.Data.ID != "original-string-id" {
			t.Fatal("legacy success/data envelope changed")
		}
	}
	admin = true
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/marketplace/admin/security-audit/events/original-string-id", strings.NewReader(`{"status":"resolved","note":"kept"}`)))
	if w.Code != 200 || calls.Load() != 2 {
		t.Fatal("string review ID not delegated")
	}
	r := httptest.NewRequest(http.MethodPatch, "/api/marketplace/security-audit/events/original-string-id", strings.NewReader(`{"status":"resolved"}`))
	r.Header.Set("Origin", "https://foreign.test")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 || calls.Load() != 2 {
		t.Fatal("cross-site review reached delegated handler")
	}
}

func TestSecurityAliasFailureNeverPretendsToBeSuccess(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if malformed {
				_, _ = w.Write([]byte("incomplete"))
				return
			}
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"audit event not found"}`))
		})
		s := New(nil, nil, nil, Config{SecurityAuditHandler: handler}, nil)
		mux := http.NewServeMux()
		s.Register(mux, func(*http.Request) (Actor, error) { return Actor{UserID: 1}, nil })
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/marketplace/security-audit/events", nil))
		if (malformed && w.Code != 503) || (!malformed && w.Code != 404) || !strings.Contains(w.Body.String(), `"success":false`) {
			t.Fatalf("false delegated success: %d %s", w.Code, w.Body.String())
		}
	}
}
