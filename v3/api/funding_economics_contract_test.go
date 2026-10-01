package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestFundingEconomicsContractRequiresRootSessionAndExactMoney(t *testing.T) {
	const path = "/api/billing/funding-economics"
	op := contractPaths(t)[path]["get"]
	if op.OperationID == "" || op.RequiredRole != "root" {
		t.Fatalf("funding economics requires an explicit root role: %#v", op)
	}
	if !reflect.DeepEqual(op.Security, []map[string][]string{{"sessionCookie": {}}, {"sessionBearer": {}}}) {
		t.Fatalf("funding economics must reject non-session credentials: %#v", op.Security)
	}
	for _, status := range []string{"200", "400", "401", "403", "503"} {
		if _, ok := op.Responses[status]; !ok {
			t.Errorf("funding economics missing status %s", status)
		}
	}
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name     string         `json:"name"`
				In       string         `json:"in"`
				Required bool           `json:"required"`
				Schema   map[string]any `json:"schema"`
			} `json:"parameters"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]map[string]any `json:"properties"`
				Required   []string                  `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	operation := document.Paths[path]["get"]
	if len(operation.Parameters) != 1 {
		t.Fatalf("funding report needs only day parameter: %#v", operation.Parameters)
	}
	day := operation.Parameters[0]
	if day.Name != "day" || day.In != "query" || day.Required || day.Schema["type"] != "string" || day.Schema["format"] != "date" || day.Schema["minLength"] != float64(10) || day.Schema["maxLength"] != float64(10) {
		t.Fatalf("funding day must be an optional strict calendar date: %#v", day)
	}
	response := operation.Responses["200"].Content["application/json"].Schema
	properties, ok := response["properties"].(map[string]any)
	if !ok || properties["success"] == nil {
		t.Fatalf("report must retain control response envelope: %#v", response)
	}
	data, ok := properties["data"].(map[string]any)
	if !ok || data["$ref"] != "#/components/schemas/FundingDailyEconomics" {
		t.Fatalf("report needs actual ledger DTO: %#v", properties["data"])
	}
	collection := document.Components.Schemas["FundingDailyEconomics"].Properties["sources"]
	if collection["type"] != "array" || collection["nullable"] == true {
		t.Fatal("DailyFundingEconomics initializes an empty source array, never null")
	}
	for name, fields := range map[string][]string{
		"FundingDailyEconomics":  {"recognized_revenue_micro", "recognized_cost_micro", "recognized_profit_micro", "unattributed_cost_micro"},
		"FundingEconomicsSource": {"amount_micro", "revenue_micro", "cost_micro", "profit_micro"},
	} {
		schema := document.Components.Schemas[name]
		for _, field := range fields {
			property := schema.Properties[field]
			if property["type"] != "integer" || property["format"] != "int64" || !containsString(schema.Required, field) {
				t.Errorf("%s.%s must retain required exact int64", name, field)
			}
			if field == "profit_micro" || field == "recognized_profit_micro" {
				if _, limited := property["minimum"]; limited {
					t.Errorf("%s.%s must allow signed losses", name, field)
				}
			} else if property["minimum"] != float64(0) {
				t.Errorf("%s.%s must document nonnegative source amount/revenue/cost", name, field)
			}
		}
	}
	got := document.Components.Schemas["FundingEconomicsSource"].Properties["source"]["enum"]
	want := []any{"topup", "blind_box", "subscription", "legacy_unattributed", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("funding sources differ from active ledger policy: got %#v", got)
	}
}

func TestGeneratedFundingDayBindingRejectsMalformedCalendarDate(t *testing.T) {
	calls := 0
	h := DomainHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.WriteHeader(http.StatusBadRequest)
	})
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"?day=2026-02-30", http.StatusBadRequest},
		{"?day=2026-1-01", http.StatusBadRequest},
		{"?day=not-a-date", http.StatusBadRequest},
		{"?day=2024-02-29", http.StatusNoContent},
		{"?day=0001-01-01", http.StatusNoContent},
		{"?day=9999-12-31", http.StatusNoContent},
		{"", http.StatusNoContent},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/billing/funding-economics"+tc.query, nil))
		if w.Code != tc.want {
			t.Errorf("funding day %q: got %d want %d", tc.query, w.Code, tc.want)
		}
	}
	if calls != 4 {
		t.Errorf("malformed dates reached the domain handler: calls=%d", calls)
	}
}
