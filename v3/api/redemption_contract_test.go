package api

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

// Inspect the published variant constraints directly; this helper is not a
// general JSON-schema validator. Domain tests enforce expiry and plan existence.
type redemptionVariantSchema struct {
	Type                 string                              `json:"type"`
	Nullable             bool                                `json:"nullable"`
	Properties           map[string]*redemptionVariantSchema `json:"properties"`
	Required             []string                            `json:"required"`
	Enum                 []string                            `json:"enum"`
	Minimum              *int64                              `json:"minimum"`
	Maximum              *int64                              `json:"maximum"`
	AdditionalProperties *bool                               `json:"additionalProperties"`
	OneOf                []*redemptionVariantSchema          `json:"oneOf"`
}

func (schema *redemptionVariantSchema) matches(raw json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return schema.Nullable
	}
	switch schema.Type {
	case "object":
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		for _, name := range schema.Required {
			if _, exists := fields[name]; !exists {
				return false
			}
		}
		for name, value := range fields {
			property, declared := schema.Properties[name]
			if !declared {
				if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
					return false
				}
			} else if !property.matches(value) {
				return false
			}
		}
	case "integer":
		value, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
		if err != nil || (schema.Minimum != nil && value < *schema.Minimum) || (schema.Maximum != nil && value > *schema.Maximum) {
			return false
		}
	case "string":
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		if len(schema.Enum) > 0 {
			found := false
			for _, allowed := range schema.Enum {
				found = found || value == allowed
			}
			if !found {
				return false
			}
		}
	}
	if len(schema.OneOf) > 0 {
		matches := 0
		for _, variant := range schema.OneOf {
			if variant.matches(raw) {
				matches++
			}
		}
		return matches == 1
	}
	return true
}

func TestTypedRedemptionIssueContractRejectsMixedAndRetiredBenefits(t *testing.T) {
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &document); err != nil {
		t.Fatal(err)
	}
	rawSchema, exists := document.Components.Schemas["IssueRedemptionInput"]
	if !exists {
		t.Fatal("missing typed redemption issuance schema")
	}
	var schema redemptionVariantSchema
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"credits":9007199254740993}`, true},
		{`{"name":"wallet","redeem_type":"credits","credits":1,"plan_id":0,"blind_box_quantity":0}`, true},
		{`{"redeem_type":"subscription","plan_id":9007199254740993,"credits":0,"expires_at":null}`, true},
		{`{"redeem_type":"subscription","plan_id":1}`, true},
		{`{"redeem_type":"blind_box","blind_box_quantity":1}`, true},
		{`{"redeem_type":"blind_box","blind_box_quantity":100,"credits":0}`, true},
		{`{"credits":0}`, false},
		{`{"credits":-1}`, false},
		{`{"credits":1,"plan_id":1}`, false},
		{`{"redeem_type":"subscription"}`, false},
		{`{"redeem_type":"subscription","plan_id":1,"credits":1}`, false},
		{`{"redeem_type":"subscription","plan_id":1,"blind_box_quantity":1}`, false},
		{`{"redeem_type":"blind_box","blind_box_quantity":0}`, false},
		{`{"redeem_type":"blind_box","blind_box_quantity":101}`, false},
		{`{"redeem_type":"blind_box","blind_box_quantity":1,"plan_id":1}`, false},
		{`{"redeem_type":"blind_box","blind_box_quantity":1,"credits":1}`, false},
		{`{"redeem_type":"quota","credits":1}`, false},
		{`{"redeem_type":"points","credits":1}`, false},
		{`{"quota":500000}`, false},
		{`{"credits":9223372036854775808}`, false},
	} {
		if got := schema.matches(json.RawMessage(tc.body)); got != tc.want {
			t.Errorf("issuance variant %s: accepted=%t, want %t", tc.body, got, tc.want)
		}
	}
}

func TestTypedRedemptionAliasesReturnObjectReceipt(t *testing.T) {
	paths := contractPaths(t)
	for _, path := range []string{"/api/user/topup", "/api/commerce/redemptions/redeem"} {
		var response struct {
			Content map[string]struct {
				Schema struct {
					Properties map[string]struct {
						Reference string `json:"$ref"`
					} `json:"properties"`
				} `json:"schema"`
			} `json:"content"`
		}
		if err := json.Unmarshal(paths[path]["post"].Responses["200"], &response); err != nil {
			t.Fatal(err)
		}
		if response.Content["application/json"].Schema.Properties["data"].Reference != "#/components/schemas/RedemptionResult" {
			t.Errorf("%s must return a typed benefit receipt rather than a scalar amount", path)
		}
	}
}
