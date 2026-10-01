package commerce

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvoiceValidationRejectsUntrustedAndOversizedInputs(t *testing.T) {
	valid := CreateInvoiceRequestInput{SourceType: "topup", TradeNo: "trade", InvoiceType: "personal", Title: "测试抬头", Email: "payer@example.test"}
	for _, test := range []struct {
		name string
		edit func(*CreateInvoiceRequestInput)
	}{
		{"foreign source", func(i *CreateInvoiceRequestInput) { i.SourceType = "wallet" }},
		{"empty title", func(i *CreateInvoiceRequestInput) { i.Title = " " }},
		{"large unicode title", func(i *CreateInvoiceRequestInput) { i.Title = strings.Repeat("字", 256) }},
		{"enterprise tax", func(i *CreateInvoiceRequestInput) { i.InvoiceType = "enterprise"; i.TaxNumber = "123" }},
		{"oversized tax", func(i *CreateInvoiceRequestInput) { i.TaxNumber = strings.Repeat("1", 65) }},
		{"display email", func(i *CreateInvoiceRequestInput) { i.Email = "Payer <payer@example.test>" }},
		{"bad email", func(i *CreateInvoiceRequestInput) { i.Email = "payer" }},
		{"oversized remark", func(i *CreateInvoiceRequestInput) { i.Remark = strings.Repeat("字", 501) }},
		{"duplicate trade with different source", func(i *CreateInvoiceRequestInput) {
			i.Orders = []InvoiceOrderInput{{"topup", "trade"}, {"subscription", "trade"}}
		}},
		{"large batch", func(i *CreateInvoiceRequestInput) { i.Orders = make([]InvoiceOrderInput, 101) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.edit(&input)
			if err := validateInvoiceCreate(&input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid input accepted: %v", err)
			}
		})
	}
	if err := validateInvoiceCreate(&valid); err != nil || len(valid.Orders) != 1 {
		t.Fatalf("single-order compatibility: %+v err=%v", valid, err)
	}
	for _, input := range []UpdateInvoiceRequestInput{{Status: "pending"}, {Status: "issued"}, {Status: "rejected"}, {Status: "rejected", InvoiceNumber: "N", AdminNote: "reason"}} {
		if err := validateInvoiceUpdate(&input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid outcome accepted: %+v %v", input, err)
		}
	}
}

func TestInvoiceAmountKeepsExactMoneyIncludingCurrencyPrecision(t *testing.T) {
	for _, test := range []struct {
		minor int64
		coin  string
		want  string
	}{{12345, "usd", "123.45"}, {12345, "jpy", "12345"}, {12345, "kwd", "12.345"}, {math.MaxInt64, "usd", "92233720368547758.07"}} {
		if got := invoiceAmount(test.minor, test.coin).String(); got != test.want {
			t.Fatalf("%s %d: %s, expected %s", test.coin, test.minor, got, test.want)
		}
	}
}

func TestInvoiceRoutesAuthenticateAndEnforceAdministrator(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
		actor  Actor
		want   int
	}{
		{"GET", "/api/invoices/eligible-orders", Actor{}, http.StatusUnauthorized},
		{"POST", "/api/invoices/requests", Actor{}, http.StatusUnauthorized},
		{"GET", "/api/invoices/admin/requests", Actor{UserID: 1, Role: "user"}, http.StatusForbidden},
		{"PUT", "/api/invoices/admin/requests/1", Actor{UserID: 1, Role: "user"}, http.StatusForbidden},
		{"PUT", "/api/invoices/admin/requests/bad", Actor{UserID: 1, Role: "admin"}, http.StatusBadRequest},
		{"POST", "/api/invoices/requests", Actor{UserID: 1, Role: "user"}, http.StatusBadRequest},
		{"GET", "/api/invoices/requests?p=bad", Actor{UserID: 1, Role: "user"}, http.StatusBadRequest},
	} {
		mux := http.NewServeMux()
		h := handler{s: New(nil, nil, nil, Config{}), authenticate: func(*http.Request) (Actor, error) { return test.actor, nil }}
		h.registerInvoices(mux)
		reply := httptest.NewRecorder()
		mux.ServeHTTP(reply, httptest.NewRequest(test.method, test.path, strings.NewReader(`{"user_id":999}`)))
		if reply.Code != test.want {
			t.Fatalf("%s %s status=%d expected=%d body=%s", test.method, test.path, reply.Code, test.want, reply.Body.String())
		}
	}
}
