package commerce

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOrderInvoicePDFPreservesExactAmountChineseAndSafeText(t *testing.T) {
	data := orderInvoiceData{number: "CG-2026-000000000001", buyer: "张三 (Buyer)\\",
		buyerAddress: "香港九龙弥敦道七百五十号", sellerAddress: "UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG",
		description: "AI subscription / 标准月卡", trade: "purchase-1",
		provider: "test)\n/JS (unsafe", currency: "USD", amount: "92233720368547758.07",
		paid:   time.Date(2026, 10, 3, 15, 40, 0, 0, time.FixedZone("HKT", 28800)),
		issued: time.Date(2026, 10, 4, 16, 50, 0, 0, time.FixedZone("HKT", 28800))}
	pdf := mustRenderOrderInvoice(t, data)
	for _, value := range []string{"%PDF-1.4", "CG-2026-000000000001", "USD 92233720368547758.07", "CodeGo AI Limited", "VAT / GST", "03 Oct 2026 15:40 HKT", "04 Oct 2026 16:50 HKT", "UNIT PRICE", invoiceUTF16(data.buyerAddress), invoiceUTF16("张三 (Buyer)\\"), invoiceUTF16("碼高智能有限公司")} {
		if !bytes.Contains(pdf, []byte(value)) {
			t.Fatalf("PDF omitted %q", value)
		}
	}
	if bytes.Contains(pdf, []byte("test)\n/JS")) || !bytes.Contains(pdf, []byte(`test\) /JS \(unsafe`)) {
		t.Fatal("PDF string injection was not escaped")
	}
	if !bytes.Equal(pdf, mustRenderOrderInvoice(t, data)) {
		t.Fatal("an unchanged order must produce a stable invoice")
	}
	// Optional local visual review output never writes personal test data by default.
	if path := os.Getenv("V3_INVOICE_PREVIEW_PATH"); path != "" {
		if err := os.WriteFile(path, pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrderInvoiceLongAddressesPaginateWithoutLosingPurchaserData(t *testing.T) {
	data := orderInvoiceData{buyer: strings.Repeat("客", 200), buyerAddress: strings.Repeat("址", 599) + "末",
		sellerAddress: strings.Repeat("店", 599) + "终", currency: "USD", amount: "92233720368547758.07"}
	pdf := mustRenderOrderInvoice(t, data)
	if !bytes.Contains(pdf, []byte("/Count 2")) || !bytes.Contains(pdf, []byte(invoiceUTF16("末"))) || !bytes.Contains(pdf, []byte(invoiceUTF16("终"))) {
		t.Fatal("long addresses must be complete and paginated")
	}
	if path := os.Getenv("V3_INVOICE_LONG_PREVIEW_PATH"); path != "" {
		if err := os.WriteFile(path, pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrderInvoiceIssuanceRequiresRealNameAndAddress(t *testing.T) {
	service := New(nil, nil, nil, Config{})
	for _, input := range []IssueOrderInvoiceInput{
		{}, {BuyerName: "Buyer"}, {BuyerAddress: "Address"},
		{BuyerName: strings.Repeat("字", 201), BuyerAddress: "Address"},
		{BuyerName: "Buyer", BuyerAddress: strings.Repeat("字", 601)},
		{BuyerName: "Buyer\nname", BuyerAddress: "Address"},
		{BuyerName: "Buyer", BuyerAddress: "Street\x00number"},
	} {
		if _, err := service.IssueOrderInvoice(t.Context(), 1, "trade", input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid purchaser data accepted: %+v %v", input, err)
		}
	}
	if !invoiceTextValid("Flat 18\nHong Kong", 600, true) {
		t.Fatal("multiline purchaser address rejected")
	}
}

func TestOrderInvoicePDFOffsetsAndStreamLength(t *testing.T) {
	pdf := mustRenderOrderInvoice(t, orderInvoiceData{currency: "JPY", amount: "12345"})
	source := string(pdf)
	xrefStart := strings.LastIndex(source, "startxref\n")
	if xrefStart < 0 {
		t.Fatal("missing PDF cross-reference")
	}
	xref, err := strconv.Atoi(strings.Split(source[xrefStart+10:], "\n")[0])
	if err != nil || !strings.HasPrefix(source[xref:], "xref\n") {
		t.Fatalf("invalid xref offset: %d %v", xref, err)
	}
	rows := strings.Split(source[xref:], "\n")
	count, err := strconv.Atoi(strings.Fields(rows[1])[1])
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows[3 : count+2] {
		offset, err := strconv.Atoi(strings.Fields(row)[0])
		if err != nil || !strings.HasPrefix(source[offset:], fmt.Sprintf("%d 0 obj\n", i+1)) {
			t.Fatalf("invalid object %d offset", i+1)
		}
	}
	start := strings.Index(source, "stream\n") + len("stream\n")
	end := strings.Index(source, "endstream")
	lengthStart := strings.LastIndex(source[:start], "/Length ") + len("/Length ")
	length, err := strconv.Atoi(strings.Fields(source[lengthStart:])[0])
	if err != nil || length != end-start {
		t.Fatalf("stream length %d, actual %d: %v", length, end-start, err)
	}
	if !bytes.Contains(pdf, []byte("JPY 12345")) {
		t.Fatal("zero-decimal currency was changed")
	}
}

func TestOrderInvoiceUnicodeEncoding(t *testing.T) {
	decoded, err := hex.DecodeString(invoiceUTF16("码高"))
	if err != nil || len(decoded) != 4 {
		t.Fatalf("Chinese text encoding: %x %v", decoded, err)
	}
}

func TestOrderInvoiceRoutesRequireSessionAndValidateTrade(t *testing.T) {
	for _, test := range []struct {
		actor Actor
		trade string
		want  int
	}{
		{Actor{}, "purchase", http.StatusUnauthorized},
		{Actor{UserID: 1, Role: "user"}, strings.Repeat("x", 257), http.StatusBadRequest},
	} {
		mux := http.NewServeMux()
		h := handler{s: New(nil, nil, nil, Config{}), authenticate: func(*http.Request) (Actor, error) { return test.actor, nil }}
		h.registerInvoices(mux)
		reply := httptest.NewRecorder()
		mux.ServeHTTP(reply, httptest.NewRequest(http.MethodGet, "/api/commerce/orders/"+test.trade+"/invoice", nil))
		if reply.Code != test.want || reply.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("status=%d, want=%d body=%s", reply.Code, test.want, reply.Body.String())
		}
	}
}
