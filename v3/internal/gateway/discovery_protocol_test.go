package gateway_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestModelDiscoveryProtocolAliasesUseNativeResponseShapes(t *testing.T) {
	f := newDiscoveryFixture(t)
	tests := []struct {
		name    string
		path    string
		headers map[string]string
		root    string
		idKey   string
	}{
		{"OpenAI", "/v1/models", discoveryBearer, "data", "id"},
		{"GeminiOpenAI", "/v1beta/openai/models", map[string]string{"X-Goog-Api-Key": "sk-test"}, "data", "id"},
		{"Anthropic", "/v1/models", map[string]string{"X-Api-Key": "sk-test", "Anthropic-Version": "2023-06-01"}, "data", "id"},
		{"Gemini", "/v1beta/models", map[string]string{"X-Goog-Api-Key": "sk-test"}, "models", "name"},
		{"GeminiQuery", "/v1beta/models?key=sk-test", nil, "models", "name"},
		{"GeminiV1", "/v1/models?key=sk-test", nil, "models", "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := f.get(tt.path, tt.headers)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			var models []map[string]any
			if err := json.Unmarshal(body[tt.root], &models); err != nil {
				t.Fatal(err)
			}
			if len(models) != 2 || models[0][tt.idKey] != "alpha" || models[1][tt.idKey] != "zeta" {
				t.Fatalf("invalid native models: %s", w.Body.String())
			}
			if tt.name == "Anthropic" && (string(body["first_id"]) != `"alpha"` || string(body["last_id"]) != `"zeta"` || string(body["has_more"]) != "false" || models[0]["created_at"] != "2021-07-20T10:40:00Z") {
				t.Fatalf("invalid Anthropic metadata: %s", w.Body.String())
			}
		})
	}
}

func TestModelDiscoveryNativeDetailsAndErrors(t *testing.T) {
	f := newDiscoveryFixture(t)
	for _, tt := range []struct {
		path    string
		headers map[string]string
		field   string
	}{
		{"/v1/models/alpha", discoveryBearer, "id"},
		{"/v1/models/alpha", map[string]string{"X-Api-Key": "sk-test", "Anthropic-Version": "2023-06-01"}, "display_name"},
		{"/v1beta/models/alpha?key=sk-test", nil, "name"},
		{"/v1beta/openai/models/alpha?key=sk-test", nil, "id"},
	} {
		w := f.get(tt.path, tt.headers)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || body[tt.field] != "alpha" {
			t.Fatalf("native detail %s: %d %s", tt.path, w.Code, w.Body.String())
		}
	}
	w := f.get("/v1beta/models/unknown?key=sk-test", nil)
	var body struct {
		Error struct {
			Code   int    `json:"code"`
			Status string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusNotFound || body.Error.Code != http.StatusNotFound || body.Error.Status != "NOT_FOUND" {
		t.Fatalf("invalid Gemini unknown-model error: %d %s", w.Code, w.Body.String())
	}
}
