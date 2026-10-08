package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/api"
)

func TestRoutePoolGroupOptionsContractUsesSessionAndPreciseMultiplier(t *testing.T) {
	w := httptest.NewRecorder()
	api.Specification(w, httptest.NewRequest("GET", "/api/openapi.json", nil))
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string          `json:"operationId"`
			Security    json.RawMessage `json:"security"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Type     string `json:"type"`
					GoType   string `json:"x-go-type"`
					Nullable bool   `json:"nullable"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	op := doc.Paths["/api/marketplace/route-pools/group-options"]["get"]
	if op.OperationID != "getApiMarketplaceRoutePoolsGroupOptions" || string(op.Security) == "[]" {
		t.Fatalf("missing protected operation: %+v", op)
	}
	schema := doc.Components.Schemas["ChannelMarketRoutePoolGroupOption"]
	if len(schema.Required) != 7 || schema.Properties["group_id"].Type != "string" || schema.Properties["display_id"].Type != "string" || schema.Properties["routing_group"].Type != "string" || schema.Properties["multiplier"].GoType != "json.Number" || schema.Properties["models"].Type != "array" || schema.Properties["models"].Nullable {
		t.Fatalf("candidate contract loses identities, multiplier precision, or nonnull arrays: %+v", schema)
	}
}
