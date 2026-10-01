package gateway_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestModelDiscoveryEnrichesVisibleModelsFromRetainedMetadata(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	snap.Metadata = catalog.MetadataSnapshot{
		Vendors: []catalog.VendorMetadata{{ID: 91, Name: "Source Vendor", Description: "vendor record", Status: 0}},
		Models: []catalog.ModelMetadata{{
			ID: 82, ModelName: "alpha", NameRule: catalog.NameRuleExact, Status: 0, VendorID: 91,
			Description: "Retained model description", Icon: "alpha-icon", Tags: "tools,reasoning",
			Endpoints: `{"openai":"/v1/chat/completions","gemini":{"path":"/v1beta/models/{model}:generateContent","method":"post"},"invalid":false}`,
		}},
	}
	for _, path := range []string{"/v1/models", "/v1/models/alpha"} {
		w := f.get(path, discoveryBearer)
		if w.Code != http.StatusOK {
			t.Fatalf("descriptive status disabled actual route: %d %s", w.Code, w.Body.String())
		}
		var model map[string]any
		if path == "/v1/models" {
			var body struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Data) != 2 {
				t.Fatalf("metadata changed availability: %s", w.Body.String())
			}
			model = body.Data[0]
		} else if err := json.Unmarshal(w.Body.Bytes(), &model); err != nil {
			t.Fatal(err)
		}
		if model["id"] != "alpha" || model["object"] != "model" || model["created"] != float64(1626777600) || model["owned_by"] != "Source Vendor" {
			t.Fatalf("standard fields changed or vendor was ignored: %v", model)
		}
		if model["description"] != "Retained model description" || model["icon"] != "alpha-icon" || model["tags"] != "tools,reasoning" {
			t.Fatalf("retained metadata lost: %v", model)
		}
		if !reflect.DeepEqual(model["supported_endpoint_types"], []any{"gemini", "openai"}) {
			t.Fatalf("actual source endpoint keys not retained: %v", model)
		}
		for _, internal := range []string{"vendor_id", "model_name", "name_rule", "status", "sync_official", "created_time", "generationMethods", "endpoints"} {
			if _, exists := model[internal]; exists {
				t.Fatalf("internal metadata field %s leaked: %v", internal, model)
			}
		}
	}
}

func TestModelDiscoveryMetadataUsesSourceMatchingPrecedenceAndCase(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	models := []catalog.ModelMetadata{
		{ID: 1, ModelName: "ph", NameRule: catalog.NameRuleContains, Description: "contains"},
		{ID: 2, ModelName: "ha", NameRule: catalog.NameRuleSuffix, Description: "suffix"},
		{ID: 3, ModelName: "al", NameRule: catalog.NameRulePrefix, Description: "prefix"},
		{ID: 4, ModelName: "alpha", NameRule: catalog.NameRuleExact, Description: "exact"},
	}
	for _, tc := range []struct {
		count       int
		description string
	}{{4, "exact"}, {3, "prefix"}, {2, "suffix"}, {1, "contains"}} {
		snap.Metadata.Models = models[:tc.count]
		w := f.get("/v1/models/alpha", discoveryBearer)
		var item struct{ ID, Description, OwnedBy string }
		if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || item.ID != "alpha" || item.Description != tc.description {
			t.Fatalf("%s precedence lost: %d %s", tc.description, w.Code, w.Body.String())
		}
	}
	snap.Metadata.Models = []catalog.ModelMetadata{{ModelName: "AL", NameRule: catalog.NameRulePrefix, Description: "must not casefold"}}
	w := f.get("/v1/models/alpha", discoveryBearer)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "must not casefold") {
		t.Fatalf("case-sensitive source matching changed: %d %s", w.Code, w.Body.String())
	}
	snap.Metadata.Vendors = []catalog.VendorMetadata{{ID: 1, Name: "  "}}
	snap.Metadata.Models = []catalog.ModelMetadata{{ModelName: "alpha", VendorID: 1}}
	w = f.get("/v1/models/alpha", discoveryBearer)
	if !strings.Contains(w.Body.String(), `"owned_by":"openai"`) {
		t.Fatalf("empty vendor did not preserve provider fallback: %s", w.Body.String())
	}
}

func TestModelDiscoveryMetadataNeverGrantsVisibility(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	snap.Metadata = catalog.MetadataSnapshot{
		Vendors: []catalog.VendorMetadata{{ID: 9, Name: "hidden vendor"}},
		Models: []catalog.ModelMetadata{
			{ID: 1, ModelName: "hidden", VendorID: 9, Description: "restricted description"},
			{ID: 2, ModelName: "metadata-only", VendorID: 9, Description: "unroutable description"},
		},
	}
	w := f.get("/v1/models", discoveryBearer)
	if ids := discoveryIDs(t, w); !reflect.DeepEqual(ids, []string{"alpha", "zeta"}) {
		t.Fatalf("metadata expanded visible models: %v", ids)
	}
	for _, text := range []string{"hidden vendor", "restricted description", "metadata-only", "unroutable description"} {
		if strings.Contains(w.Body.String(), text) {
			t.Fatalf("inaccessible metadata %q leaked: %s", text, w.Body.String())
		}
	}
	for _, name := range []string{"hidden", "metadata-only"} {
		if w := f.get("/v1/models/"+name, discoveryBearer); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "description") {
			t.Fatalf("inaccessible metadata detail: %d %s", w.Code, w.Body.String())
		}
	}
	snap.Metadata.Models = []catalog.ModelMetadata{{ModelName: "alpha", Description: "authorized description"}}
	f.auth.principal.AllowedModels = []string{}
	if w := f.get("/v1/models/alpha", discoveryBearer); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "authorized description") {
		t.Fatalf("metadata bypassed model ACL: %d %s", w.Code, w.Body.String())
	}
}

func TestModelDiscoveryNativeMetadataOnlyClaimsExplicitGoogleActions(t *testing.T) {
	f := newDiscoveryFixture(t)
	snap := f.current.Load()
	snap.Metadata.Models = []catalog.ModelMetadata{{
		ModelName: "al", NameRule: catalog.NameRulePrefix, Description: "actual description",
		Endpoints: `{"gemini":"/v1beta/models/{model}:generateContent","gemini_stream":{"path":"/v1/models/{model}:streamGenerateContent"},"openai":"/v1/chat/completions","unknown":"/v1beta/models/{model}:invented","get":{"path":"/v1beta/models/{model}:countTokens","method":"GET"}}`,
	}}
	for _, path := range []string{"/v1beta/models/alpha?key=sk-test", "/v1beta/models?key=sk-test"} {
		w := f.get(path, nil)
		var model map[string]any
		if strings.Contains(path, "/alpha") {
			if err := json.Unmarshal(w.Body.Bytes(), &model); err != nil {
				t.Fatal(err)
			}
		} else {
			var body struct {
				Models []map[string]any `json:"models"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			model = body.Models[0]
		}
		if w.Code != http.StatusOK || model["name"] != "alpha" || model["displayName"] != "alpha" || model["description"] != "actual description" ||
			!reflect.DeepEqual(model["supportedGenerationMethods"], []any{"generateContent", "streamGenerateContent"}) {
			t.Fatalf("genuine Google metadata lost or invented: %d %s", w.Code, w.Body.String())
		}
	}
	w := f.get("/v1/models/alpha", map[string]string{"X-Api-Key": "sk-test", "Anthropic-Version": "2023-06-01"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"display_name":"alpha"`) || strings.Contains(w.Body.String(), "actual description") {
		t.Fatalf("Anthropic public name replaced by source prefix or unknown field added: %d %s", w.Code, w.Body.String())
	}
	for _, endpoints := range []string{`{"openai":"/v1/chat/completions"}`, `{"gemini":false}`, `{"gemini":null}`, `{"gemini":{"path":77}}`, `["gemini"]`, `bad JSON`} {
		snap.Metadata.Models[0].Endpoints = endpoints
		w := f.get("/v1beta/models/alpha?key=sk-test", nil)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "supportedGenerationMethods") {
			t.Fatalf("opaque endpoint text invented a Google action or blocked valid route: %s %d %s", endpoints, w.Code, w.Body.String())
		}
	}
}
