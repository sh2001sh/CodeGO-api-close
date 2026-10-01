package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSecurityAuditContractRetainsGuardSemantics(t *testing.T) {
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name   string         `json:"name"`
				In     string         `json:"in"`
				Schema map[string]any `json:"schema"`
			} `json:"parameters"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties           map[string]map[string]any `json:"properties"`
				Required             []string                  `json:"required"`
				AdditionalProperties json.RawMessage           `json:"additionalProperties"`
				AnyOf                []json.RawMessage         `json:"anyOf"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	paths := contractPaths(t)
	for _, prefix := range []string{"/api/security-audit/", "/api/marketplace/security-audit/", "/api/marketplace/admin/security-audit/"} {
		canonical := prefix == "/api/security-audit/"
		for _, tc := range []struct{ suffix, method, response string }{
			{"events", "get", "SecurityAuditEventList"},
			{"events/export", "get", ""},
			{"events/{id}", "patch", "SecurityAuditEvent"},
		} {
			path := prefix + tc.suffix
			op := paths[path][tc.method]
			if !reflect.DeepEqual(op.Security, []map[string][]string{{"sessionCookie": {}}, {"sessionBearer": {}}}) {
				t.Errorf("%s mixes session and other credential classes: %#v", path, op.Security)
			}
			adminAlias := strings.Contains(prefix, "/admin/")
			if (op.RequiredRole == "admin") != adminAlias {
				t.Errorf("%s has incorrect role restriction %q", path, op.RequiredRole)
			}
			contract := document.Paths[path][tc.method]
			if tc.response == "" {
				if _, ok := contract.Responses["200"].Content["text/csv"]; !ok {
					t.Errorf("%s is not a CSV export", path)
				}
			} else {
				schema := contract.Responses["200"].Content["application/json"].Schema
				if !canonical {
					properties, ok := schema["properties"].(map[string]any)
					if !ok || properties["success"] == nil {
						t.Fatalf("%s must retain its envelope", path)
					}
					schema, ok = properties["data"].(map[string]any)
					if !ok {
						t.Fatalf("%s lacks typed data", path)
					}
				}
				if schema["$ref"] != "#/components/schemas/"+tc.response {
					t.Errorf("%s has wrong DTO: %#v", path, schema)
				}
			}
			if canonical {
				for _, status := range []string{"400", "401", "403", "404", "503"} {
					schema := contract.Responses[status].Content["application/json"].Schema
					if schema["$ref"] != "#/components/schemas/SecurityAuditError" {
						t.Errorf("%s %s must use a bare Guard error", path, status)
					}
				}
			}
			queries := map[string]map[string]any{}
			for _, parameter := range contract.Parameters {
				if parameter.In == "path" && parameter.Name == "id" && parameter.Schema["type"] != "string" {
					t.Errorf("%s converted the original event ID", path)
				}
				if parameter.In == "query" {
					queries[parameter.Name] = parameter.Schema
				}
			}
			if tc.method == "get" {
				for _, name := range []string{"source", "review_status", "marketplace_channel", "model", "search", "start_timestamp", "end_timestamp", "page", "page_size"} {
					if queries[name] == nil {
						t.Errorf("%s omits filter %s", path, name)
					}
				}
				defaultSize := float64(20)
				if canonical {
					defaultSize = 1
				} else if queries["channel_id"]["type"] != "string" {
					t.Errorf("%s lost legacy string channel_id", path)
				}
				if queries["page_size"]["default"] != defaultSize || queries["page_size"]["maximum"] != float64(100) {
					t.Errorf("%s has incorrect pagination bounds/default", path)
				}
			}
		}
		if _, detail := paths[prefix+"events/{id}"]["get"]; detail != canonical {
			t.Errorf("%s detail operation differs from registered routes", prefix)
		}
	}
	canonicalDetail := document.Paths["/api/security-audit/events/{id}"]["get"].Responses["200"].Content["application/json"].Schema
	if canonicalDetail["$ref"] != "#/components/schemas/SecurityAuditEvent" {
		t.Errorf("canonical detail must return bare Event: %#v", canonicalDetail)
	}
	event := document.Components.Schemas["SecurityAuditEvent"]
	for _, field := range []string{"id", "user_id", "token_id", "channel_id", "owner_user_id", "reviewed_by"} {
		if event.Properties[field]["type"] != "string" {
			t.Errorf("security Event.%s lost its string representation", field)
		}
	}
	for _, field := range []string{"notified_at", "reviewed_at", "created_at", "updated_at"} {
		if event.Properties[field]["nullable"] != true || !containsString(event.Required, field) {
			t.Errorf("security Event.%s must preserve required null timestamps", field)
		}
	}
	review := document.Components.Schemas["SecurityAuditReviewInput"]
	for _, field := range []string{"status", "note", "review_status", "review_note"} {
		if review.Properties[field]["type"] != "string" {
			t.Errorf("review lost actual field %s", field)
		}
	}
	if string(review.AdditionalProperties) != "true" || len(review.AnyOf) != 2 {
		t.Fatal("review must document ignored unknown fields and either status spelling")
	}
	if _, stale := document.Components.Schemas["ChannelMarketSecurityEvent"]; stale {
		t.Fatal("obsolete numeric security DTO remains published")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestGeneratedRouterPreservesSecurityAuditStringID(t *testing.T) {
	const original = "retained:security-event-uuid"
	var id string
	h := DomainHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = r.PathValue("id")
		w.WriteHeader(http.StatusNoContent)
	}), func(w http.ResponseWriter, _ *http.Request, err error) {
		t.Errorf("original string event ID rejected: %v", err)
		w.WriteHeader(http.StatusBadRequest)
	})
	for _, method := range []string{"GET", "PATCH"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/security-audit/events/"+original, nil))
		if w.Code != http.StatusNoContent || id != original {
			t.Fatalf("%s event ID changed: status=%d id=%q", method, w.Code, id)
		}
	}
}

func TestSecurityReviewContractRespectsStatusPrecedence(t *testing.T) {
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	var schema struct {
		redemptionVariantSchema
		AnyOf []*redemptionVariantSchema `json:"anyOf"`
	}
	if err := json.Unmarshal(document.Components.Schemas["SecurityAuditReviewInput"], &schema); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"status":"unreviewed"}`, true},
		{`{"status":"acknowledged","note":"已确认"}`, true},
		{`{"status":"resolved","review_status":"ignored-alias","note":"优先备注","review_note":"别名备注"}`, true},
		{`{"review_status":"false_positive","review_note":"误报"}`, true},
		{`{"status":"","review_status":"resolved","note":"","review_note":"别名备注"}`, true},
		{`{"status":"resolved","unrecognized":"ignored by decoder"}`, true},
		{`{}`, false},
		{`{"note":"missing status"}`, false},
		{`{"status":""}`, false},
		{`{"status":"invalid","review_status":"resolved"}`, false},
		{`{"review_status":"invalid"}`, false},
		{`{"status":1}`, false},
		{`{"status":"resolved","note":1}`, false},
	} {
		raw := json.RawMessage(tc.body)
		accepted := false
		if schema.matches(raw) {
			for _, alternative := range schema.AnyOf {
				accepted = accepted || alternative.matches(raw)
			}
		}
		if accepted != tc.want {
			t.Errorf("review body %s: accepted=%t, want %t", tc.body, accepted, tc.want)
		}
		if tc.want {
			var generated SecurityAuditReviewInput
			if err := json.Unmarshal(raw, &generated); err != nil {
				t.Fatalf("generated review cannot decode valid body: %v", err)
			}
			after, err := json.Marshal(generated)
			if err != nil {
				t.Fatal(err)
			}
			var beforeFields, afterFields map[string]any
			if err := json.Unmarshal(raw, &beforeFields); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(after, &afterFields); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeFields, afterFields) {
				t.Errorf("generated review loses wire fields: before=%s after=%s", raw, after)
			}
		}
	}
}
