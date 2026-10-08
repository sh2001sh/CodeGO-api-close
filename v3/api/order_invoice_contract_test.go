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
