package commerce

import (
	"fmt"
	"testing"
)

func TestOrderInvoiceCorrectionsPreserveCurrencyPrecisionAcrossRevisions(t *testing.T) {
	for _, test := range []struct {
		currency  string
		amount    int64
		formatted string
	}{
		{"JPY", 12345, "12345"}, {"jpy", 12345, "12345"}, {"USDT", 12345678, "12.345678"}, {"BHD", 12345, "12.345"}, {"USD", 12345, "123.45"},
	} {
		t.Run(test.currency, func(t *testing.T) {
			d, err := invoiceDataFromStored(storedOrderInvoice{snapshot: []byte(fmt.Sprintf(`{"amount_minor":%d,"currency":%q}`, test.amount, test.currency))})
			if err != nil || d.amount != test.formatted {
				t.Fatalf("amount %s want %s: %v", d.amount, test.formatted, err)
			}
		})
	}
}
