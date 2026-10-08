package commerce

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-text/typesetting/di"
)

func mustRenderOrderInvoice(t *testing.T, data orderInvoiceData) []byte {
	t.Helper()
	pdf, err := renderOrderInvoice(data)
	if err != nil {
		t.Fatal(err)
	}
	return pdf
}

func TestOrderInvoiceRejectsUnsupportedCharacterWithoutCorruptPDF(t *testing.T) {
	for _, data := range []orderInvoiceData{{buyer: "Buyer 😀"}, {buyerAddress: "Address 🦄"}, {description: "Plan 🚀"}} {
		pdf, err := renderOrderInvoice(data)
		if !errors.Is(err, ErrInvalid) || len(pdf) != 0 {
			t.Fatalf("unsupported input silently produced a document: pdf=%d err=%v", len(pdf), err)
		}
	}
}

func TestOrderInvoiceEmbeddedFontsCoverSupportedLanguagesAndShapeArabic(t *testing.T) {
	r := newInvoiceFontRenderer()
	for _, value := range []string{"简体中文香港", "繁體中文香港", "日本語東京", "한국어 서울", "Русский Москва", "Français éèç", "Deutsch München", "العربية محمد أحمد"} {
		for _, run := range r.runs(value, 11) {
			for _, glyph := range run.Glyphs {
				if glyph.GlyphID == 0 {
					t.Fatalf("missing glyph in supported invoice language: %q", value)
				}
			}
		}
	}
	const arabic = "محمد أحمد"
	var contextual, rtl bool
	for _, run := range r.runs(arabic, 11) {
		rtl = rtl || run.Direction.Progression() == di.TowardTopLeft
		for _, glyph := range run.Glyphs {
			nominal, _ := run.Face.NominalGlyph([]rune(arabic)[glyph.TextIndex()])
			contextual = contextual || nominal != glyph.GlyphID
		}
	}
	if !rtl || !contextual {
		t.Fatal("Arabic must use RTL layout and contextual joined letter glyphs")
	}
	runs := r.runs("محمد أحمد / Account 42", 11)
	if len(runs) < 2 || runs[0].Direction != di.DirectionLTR || runs[len(runs)-1].Direction != di.DirectionRTL {
		t.Fatal("a first-strong RTL buyer field must visually place its Latin suffix on the left")
	}
}

func TestOrderInvoiceMultilingualPDFHasEmbeddedSubsetsAndLogicalText(t *testing.T) {
	data := orderInvoiceData{number: "CG-2026-000000000042", buyer: "محمد أحمد / 한국 고객 / 山田太郎 / Élodie Müller / Иван Петров",
		buyerAddress: "香港九龍旺角彌敦道750號 / 서울 중구 / Москва / 42 شارع الملك",
		buyerCountry: "Hong Kong 香港", buyerTaxID: "BUSINESS-42", sellerBRN: "81318858",
		sellerAddress: "UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG",
		description:   "AI subscription / 標準月卡 / 月額プラン / 월간 요금제",
		trade:         "internal-order-42", provider: "stripe", paymentReference: "pi_external_transaction_42",
		currency: "USD", amount: "123.45", details: []string{"Service period: 08 Oct 2026 - 07 Nov 2026", "Account credit: USD 123.45 equivalent; separate from payment currency."},
		paid: time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC), issued: time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)}
	pdf := mustRenderOrderInvoice(t, data)
	for _, value := range []string{"/Subtype /Type3", "/CharProcs", "/ToUnicode", "/ActualText <feff" + invoiceUTF16(data.buyer), invoiceUTF16(data.buyerAddress), invoiceUTF16("Provider transaction: " + data.paymentReference), "81318858", "Valid without a signature or company chop"} {
		if !bytes.Contains(pdf, []byte(value)) {
			t.Fatalf("invoice omitted embedded glyphs or metadata: %q", value)
		}
	}
	if bytes.Contains(pdf, []byte("STSong-Light")) || len(pdf) > 512*1024 {
		t.Fatalf("invoice relies on viewer CJK fonts or embeds full font files: %d bytes", len(pdf))
	}
	if !bytes.Equal(pdf, mustRenderOrderInvoice(t, data)) {
		t.Fatal("multilingual PDF must be deterministic")
	}
	if path := os.Getenv("V3_INVOICE_MULTILINGUAL_PREVIEW_PATH"); path != "" {
		if err := os.WriteFile(path, pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrderInvoiceCreditNoteAndCorrectionReferences(t *testing.T) {
	data := orderInvoiceData{number: "CG-CN-2026-000000000001", documentType: "credit_note", relatedNumber: "CG-2026-000000000001",
		buyer: "Buyer", buyerAddress: "Address", currency: "USD", amount: "12.34", correctionReason: "Confirmed partial refund",
		sellerAddress: "UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG", description: "Partial refund of API account credit",
		trade: "internal-order-1", provider: "stripe", paymentReference: "pi_test_1", paid: time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC), issued: time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)}
	pdf := mustRenderOrderInvoice(t, data)
	for _, value := range []string{"CREDIT NOTE", "Total credited", "REFUNDED", invoiceUTF16("Related invoice: " + data.relatedNumber), invoiceUTF16("Reason: " + data.correctionReason), "USD 12.34"} {
		if !bytes.Contains(pdf, []byte(value)) {
			t.Fatalf("credit note omitted %q", value)
		}
	}
	if bytes.Contains(pdf, []byte("Total paid")) || bytes.Contains(pdf, []byte("Balance due")) {
		t.Fatal("credit note must not suggest a new payment or balance due")
	}
	if path := os.Getenv("V3_INVOICE_CREDIT_PREVIEW_PATH"); path != "" {
		if err := os.WriteFile(path, pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrderInvoiceMeasuredWrappingPreservesText(t *testing.T) {
	r := newInvoiceFontRenderer()
	for _, value := range []string{strings.Repeat("完整地址", 150) + "末", "Élodie Müller " + strings.Repeat("é", 120), "محمد أحمد " + strings.Repeat("العربية ", 40)} {
		rows := r.wrap(value, 9, 507)
		if strings.Join(rows, "") != value {
			t.Fatal("measured wrapping lost customer data")
		}
		for _, row := range rows {
			if r.width(row, 9) > 507.1 {
				t.Fatalf("wrapped line exceeds content area: %.2f", r.width(row, 9))
			}
		}
	}
}

func TestOrderInvoiceFontSubsetsSplitBefore256Glyphs(t *testing.T) {
	r := newInvoiceFontRenderer()
	var stream strings.Builder
	var value strings.Builder
	for ch := rune(0x4e00); ch < 0x4e00+600; ch++ {
		value.WriteRune(ch)
	}
	r.text(&stream, 9, 44, 600, value.String())
	if len(r.subsets) < 3 {
		t.Fatalf("glyph subsets must fit one-byte PDF encoding: %d", len(r.subsets))
	}
	for _, subset := range r.subsets {
		if len(subset) > 255 {
			t.Fatalf("overflowed embedded font subset: %d", len(subset))
		}
	}
	var objects []string
	r.appendFonts(&objects)
	for _, object := range objects {
		for _, line := range strings.Split(object, "\n") {
			if strings.HasSuffix(line, " beginbfchar") {
				count, err := strconv.Atoi(strings.Fields(line)[0])
				if err != nil || count > 100 {
					t.Fatalf("PDF Unicode mapping block exceeds 100 entries: %s", line)
				}
			}
		}
	}
}

func TestOrderInvoiceParallelRenderingIsDeterministic(t *testing.T) {
	data := orderInvoiceData{buyer: "محمد أحمد / 한국 고객 / 客戶", buyerAddress: "Hong Kong 香港", currency: "USD", amount: "12.34"}
	want := mustRenderOrderInvoice(t, data)
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			for range 3 {
				pdf, err := renderOrderInvoice(data)
				if err != nil || !bytes.Equal(pdf, want) {
					t.Errorf("concurrent document rendering changed output: %v", err)
				}
			}
		})
	}
	group.Wait()
}
