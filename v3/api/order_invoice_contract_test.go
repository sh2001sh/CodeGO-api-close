package api

import (
	"encoding/json"
	"testing"
)

func TestOrderInvoiceDocumentsRawPDFAndOwnedSessionScope(t *testing.T) {
	for _, method := range []string{"get", "post"} {
		t.Run(method, func(t *testing.T) {
			assertInvoicePDFContract(t, method)
		})
	}
}

func assertInvoicePDFContract(t *testing.T, method string) {
	t.Helper()
	op := contractPaths(t)["/api/commerce/orders/{trade_no}/invoice"][method]
	if len(op.Security) != 2 {
		t.Fatal("invoice download must require a session, never an API key")
	}
	for i, name := range []string{"sessionCookie", "sessionBearer"} {
		if len(op.Security[i]) != 1 {
			t.Fatalf("invoice download mixes credential requirements: %#v", op.Security)
		}
		if _, ok := op.Security[i][name]; !ok {
			t.Fatalf("invoice download lacks %s", name)
		}
	}
	var success struct {
		Content map[string]struct {
			Schema struct {
				Type   string `json:"type"`
				Format string `json:"format"`
			} `json:"schema"`
		} `json:"content"`
	}
	if err := json.Unmarshal(op.Responses["200"], &success); err != nil {
		t.Fatal(err)
	}
	schema := success.Content["application/pdf"].Schema
	if len(success.Content) != 1 || schema.Type != "string" || schema.Format != "binary" {
		t.Fatal("invoice download must document a binary PDF, not a JSON wrapper")
	}
	for _, status := range []string{"400", "401", "404", "409", "500"} {
		if _, ok := op.Responses[status]; !ok {
			t.Fatalf("invoice download lacks its %s failure response", status)
		}
	}
	if method == "get" {
		if _, ok := op.Responses["428"]; !ok {
			t.Fatal("GET must document missing purchaser details")
		}
	}
}

func TestInvoiceCorrectionsRequireConcurrencyAndReplayIdentity(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					MaxLength int `json:"maxLength"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specification, &spec); err != nil {
		t.Fatal(err)
	}
	input := spec.Components.Schemas["OrderInvoiceCorrectionInput"]
	for _, field := range []string{"buyer_name", "buyer_address", "previous_number", "request_id", "reason"} {
		found := false
		for _, required := range input.Required {
			found = found || required == field
		}
		if !found {
			t.Fatalf("correction omitted required %s", field)
		}
	}
	for field, limit := range map[string]int{"buyer_name": 200, "buyer_address": 600, "buyer_country": 100, "buyer_tax_id": 64, "previous_number": 64, "request_id": 128, "reason": 300} {
		if input.Properties[field].MaxLength != limit {
			t.Fatalf("incorrect %s limit", field)
		}
	}
	for path, method := range map[string]string{
		"/api/commerce/orders/{trade_no}/invoice/corrections": "post",
		"/api/commerce/orders/{trade_no}/credit-note":         "post",
		"/api/commerce/orders/{trade_no}/invoice/documents":   "get",
	} {
		op := contractPaths(t)[path][method]
		if len(op.Security) != 2 || op.Security[0]["sessionCookie"] == nil || op.Security[1]["sessionBearer"] == nil {
			t.Fatalf("document route must remain session-only: %s", path)
		}
		if _, exists := op.Responses["409"]; !exists {
			t.Fatalf("document route omits state conflict: %s", path)
		}
	}
	if _, exists := contractPaths(t)["/api/commerce/orders/{trade_no}/credit-note"]["get"]; exists {
		t.Fatal("creating a refund document must not use GET")
	}
}
