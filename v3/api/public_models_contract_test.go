package api

import (
	"encoding/json"
	"testing"
)

func TestPublicModelCatalogContractDoesNotExposeAdminMetadata(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &doc); err != nil {
		t.Fatal(err)
	}
	if operation, exists := doc.Paths["/api/public/models"]["get"]; !exists || operation.Security == nil || len(operation.Security) != 0 {
		t.Fatal("public model quotations require an explicitly public GET contract")
	}
	for _, model := range []string{"PublicModelCatalog", "PublicModelGroup", "PublicModelPrice"} {
		properties := doc.Components.Schemas[model].Properties
		if len(properties) == 0 {
			t.Fatalf("%s contract is missing", model)
		}
		for _, private := range []string{"base_url", "api_key", "credentials", "settings", "rules", "procurement_cost_multiplier_ppm", "owner_user_id", "internal_channel_id"} {
			if _, exists := properties[private]; exists {
				t.Fatalf("%s exposes private property %s", model, private)
			}
		}
	}
}
