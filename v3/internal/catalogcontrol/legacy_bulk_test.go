package catalogcontrol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyBulkValidationBeforeStorage(t *testing.T) {
	s := New(nil, nil, nil)
	for _, test := range []struct {
		name, body string
		handler    http.HandlerFunc
	}{
		{"empty deletion", `{"ids":[]}`, s.legacyDeleteChannelBatch},
		{"negative deletion", `{"ids":[1,-1]}`, s.legacyDeleteChannelBatch},
		{"unknown deletion field", `{"ids":[1],"all":true}`, s.legacyDeleteChannelBatch},
		{"invalid tag identifiers", `{"ids":[0],"tag":"tag"}`, s.legacyBatchSetChannelTag},
		{"oversized batch tag", `{"ids":[1],"tag":"` + strings.Repeat("t", 256) + `"}`, s.legacyBatchSetChannelTag},
		{"empty enable tag", `{"tag":" "}`, s.legacyEnableTagChannels},
		{"empty disable tag", `{"tag":""}`, s.legacyDisableTagChannels},
		{"negative tag weight", `{"tag":"tag","weight":-1}`, s.legacyEditTagChannels},
		{"oversized tag priority", `{"tag":"tag","priority":2147483648}`, s.legacyEditTagChannels},
		{"array override", `{"tag":"tag","param_override":"[]"}`, s.legacyEditTagChannels},
		{"null override", `{"tag":"tag","header_override":"null"}`, s.legacyEditTagChannels},
		{"nonstring mapping", `{"tag":"tag","model_mapping":"{\"model\":1}"}`, s.legacyEditTagChannels},
		{"invalid membership", `{"tag":"tag","models":"` + strings.Repeat("m", 256) + `"}`, s.legacyEditTagChannels},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			test.handler(w, httptest.NewRequest("POST", "/", strings.NewReader(test.body)))
			if w.Code != 400 {
				t.Fatalf("status %d, expected 400: %s", w.Code, w.Body.String())
			}
		})
	}
	for _, path := range []string{"/api/channel/copy/1?reset_balance=invalid", "/api/channel/copy/1?suffix=" + strings.Repeat("x", 256)} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, nil)
		r.SetPathValue("id", "1")
		s.legacyCopyChannel(w, r)
		if w.Code != 400 {
			t.Fatalf("copy validation status %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.legacyGetTagModels(w, httptest.NewRequest("GET", "/?tag=", nil))
	if w.Code != 400 {
		t.Fatalf("empty models tag status %d", w.Code)
	}
}

func TestLegacyBulkRoutesRequireAuthorization(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, nil)
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/channel/batch"}, {"DELETE", "/api/channel/disabled"},
		{"POST", "/api/channel/batch/tag"}, {"POST", "/api/channel/tag/disabled"},
		{"POST", "/api/channel/tag/enabled"}, {"PUT", "/api/channel/tag"},
		{"GET", "/api/channel/tag/models"}, {"POST", "/api/channel/copy/1"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status %d, want 403", route.method, route.path, w.Code)
		}
	}
}

func TestLegacyBulkRejectsCrossOriginMutations(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/channel/batch"}, {"DELETE", "/api/channel/disabled"},
		{"POST", "/api/channel/batch/tag"}, {"POST", "/api/channel/tag/disabled"},
		{"POST", "/api/channel/tag/enabled"}, {"PUT", "/api/channel/tag"},
		{"POST", "/api/channel/copy/1"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://untrusted.example")
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s cross-origin mutation: status %d, want 403", route.method, route.path, w.Code)
		}
	}
}
