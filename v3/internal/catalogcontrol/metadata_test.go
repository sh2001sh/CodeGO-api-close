package catalogcontrol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetadataPrefillRejectsWrongShapeAndWhitespace(t *testing.T) {
	for _, tc := range []struct {
		kind, items string
		want        bool
	}{
		{"model", `["gpt","claude"]`, true}, {"tag", `[]`, true},
		{"model", `[1]`, false}, {"tag", `[" "]`, false},
		{"model", `null`, false}, {"model", `{}`, false},
		{"endpoint", `{"openai":{"path":"/v1/chat/completions"}}`, true},
		{"endpoint", `["openai"]`, true}, {"endpoint", `true`, false},
		{"retired-pet", `[]`, false}, {"endpoint", ``, false},
	} {
		if got := validPrefillItems(tc.kind, []byte(tc.items)); got != tc.want {
			t.Errorf("%s %s: %t, want %t", tc.kind, tc.items, got, tc.want)
		}
	}
}

func TestMetadataWriteRejectsPathConflictsAndInvalidPayloadBeforeDatabase(t *testing.T) {
	server := New(nil, nil, nil)
	for _, tc := range []struct {
		method, path, body string
		handler            http.HandlerFunc
		id                 string
	}{
		{"POST", "/api/models/", `{"model_name":"x","name_rule":4}`, server.metadataSaveModel, ""},
		{"POST", "/api/models/", `{"model_name":"x","status":2147483648}`, server.metadataSaveModel, ""},
		{"POST", "/api/models/", `{"model_name":"x","sync_official":2147483648}`, server.metadataSaveModel, ""},
		{"POST", "/api/vendors/", `{"name":"x","status":2147483648}`, server.metadataSaveVendor, ""},
		{"POST", "/api/models/", `{"model_name":"x","id":2}`, server.metadataSaveModel, ""},
		{"PUT", "/api/catalog/models/2", `{"id":3,"model_name":"x"}`, server.metadataSaveModel, "2"},
		{"PUT", "/api/models/", `{"model_name":"x"}`, server.metadataSaveModel, ""},
		{"POST", "/api/vendors/", `{"name":"  "}`, server.metadataSaveVendor, ""},
		{"POST", "/api/prefill_group/", `{"name":"a","type":"tag","items":[7]}`, server.metadataSavePrefill, ""},
		{"POST", "/api/models/", `{"model_name":"a"} {}`, server.metadataSaveModel, ""},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.SetPathValue("id", tc.id)
		w := httptest.NewRecorder()
		tc.handler(w, r)
		if w.Code != 400 {
			t.Errorf("%s %s %s: %d: %s", tc.method, tc.path, tc.body, w.Code, w.Body.String())
		}
	}
}

func TestMetadataRoutesUseSpecificPathsAndPreservePriceConsumer(t *testing.T) {
	server := New(nil, nil, nil)
	routes := map[string]http.HandlerFunc{}
	server.registerMetadataRoutes(routes)
	mux := http.NewServeMux()
	for pattern := range routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	}
	for _, path := range []string{"/api/models/", "/api/models", "/api/models/missing", "/api/vendors/search", "/api/catalog/models", "/api/catalog/prices", "/api/prefill_group/", "/api/prefill_group", "/api/models/sync_upstream/preview"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 204 {
			t.Errorf("missing route %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/vendors/1/accidental-path", nil))
	if w.Code != 404 {
		t.Fatalf("legacy collection swallowed nested path: %d", w.Code)
	}
}
