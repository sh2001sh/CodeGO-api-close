package commerce

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// Filled paths from web/app/public/brand/codego-logo.svg in its 32x32 viewBox.
// SVG's rounded arcs use cubic curves here; the PDF stays vector at any zoom.
// The transform flips SVG's downward Y axis and places the mark beside the name.
const invoiceLogoVector = `q
1 0 0 -1 44 804 cm
0.094118 0.094118 0.105882 rg
26 4 m 12 4 l
6.48 4 2 8.48 2 14 c
2 18 l
2 23.52 6.48 28 12 28 c
26 28 l
27.104569 28 28 27.104569 28 26 c
28 24 l
28 22.895431 27.104569 22 26 22 c
12 22 l
9.790861 22 8 20.209139 8 18 c
8 14 l
8 11.790861 9.790861 10 12 10 c
26 10 l
27.104569 10 28 9.104569 28 8 c
28 6 l
28 4.895431 27.104569 4 26 4 c
h f
17 13 m 23 13 l 23 19 l 17 19 l
15.343146 19 14 17.656854 14 16 c
14 14.343146 15.343146 13 17 13 c
h f
0.662745 0.282353 0.145098 rg
26.5 13 m 29.5 13 l
30.328427 13 31 13.671573 31 14.5 c
31 17.5 l
31 18.328427 30.328427 19 29.5 19 c
26.5 19 l
25.671573 19 25 18.328427 25 17.5 c
25 14.5 l
25 13.671573 25.671573 13 26.5 13 c
h f
Q
`

// Invoices are deterministic documents with offline multilingual glyph subsets.
// Every validated purchaser field is printed in full and may continue on a new page.
func renderOrderInvoice(v orderInvoiceData) ([]byte, error) {
	fonts := newInvoiceFontRenderer()
	values := []string{v.number, v.buyer, v.buyerAddress, v.sellerAddress, v.description, v.trade, v.provider,
		v.currency, v.amount, v.sellerBRN, v.paymentReference, v.website, v.buyerCountry, v.buyerTaxID,
		v.relatedNumber, v.correctionReason}
	values = append(values, v.details...)
	for _, value := range values {
		if err := fonts.validate(value); err != nil {
			return nil, err
		}
	}
	var stream strings.Builder
	var pages []string
	text := func(face string, size, x, y float64, value string) {
		if face == "CJK" {
			fonts.text(&stream, size, x, y, value)
			return
		}
		fmt.Fprintf(&stream, "BT /%s %.1f Tf %.1f %.1f Td (%s) Tj ET\n", face, size, x, y, invoicePDFLiteral(value))
	}
	line := func(y float64) {
		fmt.Fprintf(&stream, "0.88 0.88 0.87 RG 0.6 w 44 %.1f m 551 %.1f l S\n", y, y)
	}
	credit := v.documentType == "credit_note"
	title, badge := "INVOICE", "PAID"
	if credit {
		title, badge = "CREDIT NOTE", "REFUNDED"
	}
	brn := v.sellerBRN
	if brn == "" {
		brn = "81318858"
	}
	hkt := time.FixedZone("HKT", 28800)
	y := 0.0
	newPage := func() {
		stream.WriteString("0.13 0.14 0.15 rg\n")
		stream.WriteString(invoiceLogoVector)
		text("Bold", 18, 87, 779, "CodeGo AI")
		titleSize := 25.0
		if credit {
			titleSize = 21
		}
		text("Bold", titleSize, 551-float64(len(title))*titleSize*0.58, 779, title)
		text("Regular", 10, 44, 746, "CodeGo AI Limited")
		text("CJK", 10, 44, 729, "碼高智能有限公司")
		text("Regular", 9, 44, 710, "Business Registration No.: "+brn)
		stream.WriteString("0.93 0.95 0.93 rg 461 739 90 21 re f\n0.24 0.35 0.27 rg\n")
		text("Bold", 9, 472, 746, badge)
		stream.WriteString("0.13 0.14 0.15 rg\n")
		text("Regular", 9, 339, 725, "Document no.")
		text("Mono", 9, 339, 709, v.number)
		text("Regular", 8, 44, 689, "Issue date: "+v.issued.In(hkt).Format("02 Jan 2006 15:04 HKT"))
		dateLabel := "Payment date (Hong Kong time)"
		if credit {
			dateLabel = "Refund confirmed date (Hong Kong time)"
		}
		text("Regular", 8, 339, 689, dateLabel)
		text("Regular", 9, 339, 674, v.paid.In(hkt).Format("02 Jan 2006 15:04 HKT"))
		line(656)
		y = 630
	}
	ensure := func(height float64) {
		if y-height < 135 {
			pages = append(pages, stream.String())
			stream.Reset()
			newPage()
		}
	}
	block := func(value string, size, width, gap float64) {
		for _, row := range fonts.wrap(value, size, width) {
			ensure(gap)
			text("CJK", size, 44, y, row)
			y -= gap
		}
	}
	label := func(value string) {
		ensure(35)
		text("Bold", 9, 44, y, value)
		y -= 20
	}
	newPage()
	label("ISSUED BY")
	block(v.sellerAddress, 9, 507, 13)
	if v.website != "" {
		block(v.website, 9, 507, 13)
	}
	y -= 15
	label("BILLED TO")
	block(v.buyer, 11, 507, 16)
	block(v.buyerAddress, 9, 507, 13)
	if v.buyerCountry != "" {
		block("Country / region: "+v.buyerCountry, 9, 507, 13)
	}
	if v.buyerTaxID != "" {
		block("Buyer tax / registration no.: "+v.buyerTaxID, 9, 507, 13)
	}
	y -= 15
	label("PAYMENT AND DOCUMENT REFERENCES")
	// The station's trade number and payment processor's transaction number
	// are distinct. Neither is truncated or incorrectly labelled as the other.
	block("Order reference: "+v.trade, 9, 507, 13)
	ensure(13)
	text("Regular", 9, 44, y, "Payment provider: "+v.provider)
	y -= 13
	if v.paymentReference != "" {
		block("Provider transaction: "+v.paymentReference, 9, 507, 13)
	}
	if v.relatedNumber != "" {
		block("Related invoice: "+v.relatedNumber, 9, 507, 13)
	}
	if v.correctionReason != "" {
		block("Reason: "+v.correctionReason, 9, 507, 13)
	}
	y -= 20
	ensure(95)
	fmt.Fprintf(&stream, "0.96 0.96 0.95 rg 44 %.1f 507 29 re f\n0.13 0.14 0.15 rg\n", y-9)
	text("Bold", 9, 56, y+2, "DESCRIPTION")
	text("Bold", 9, 270, y+2, "QTY")
	text("Bold", 9, 348, y+2, "UNIT PRICE")
	text("Bold", 9, 491, y+2, "AMOUNT")
	y -= 30
	amount := v.currency + " " + v.amount
	priceSize := 8.0
	if len(amount) > 22 {
		priceSize = 7.5
	}
	text("Regular", 10, 280, y, "1")
	text("Mono", priceSize, 418-float64(len(amount))*priceSize*0.6, y, amount)
	text("Mono", priceSize, 540-float64(len(amount))*priceSize*0.6, y, amount)
	for _, row := range fonts.wrap(v.description, 10, 202) {
		ensure(15)
		text("CJK", 10, 56, y, row)
		y -= 15
	}
	y -= 12
	for _, detail := range v.details {
		block(detail, 9, 507, 13)
	}
	y -= 16
	ensure(141)
	line(y)
	y -= 24
	text("Regular", 10, 339, y, "Subtotal")
	text("Mono", 10, 551-float64(len(amount))*6, y, amount)
	y -= 24
	text("Regular", 9, 339, y, "VAT / GST")
	text("Regular", 8, 426, y, "Not charged on this order")
	y -= 17
	line(y)
	y -= 23
	totalLabel := "Total paid"
	if credit {
		totalLabel = "Total credited"
	}
	text("Bold", 12, 339, y, totalLabel)
	y -= 26
	text("Mono", 12, 551-float64(len(amount))*7.2, y, amount)
	if !credit {
		y -= 27
		text("Regular", 10, 339, y, "Balance due")
		text("Mono", 10, 551-float64(len(v.currency+" 0"))*6, y, v.currency+" 0")
	}
	pages = append(pages, stream.String())
	for i := range pages {
		stream.Reset()
		text("Regular", 8, 44, 88, "Hong Kong commercial document. Reimbursement rules depend on your jurisdiction.")
		text("Regular", 8, 44, 74, "Electronically issued. Valid without a signature or company chop.")
		text("Regular", 8, 44, 58, "CodeGo AI Limited / Hong Kong / BRN "+brn)
		text("Regular", 8, 512, 58, fmt.Sprintf("%d / %d", i+1, len(pages)))
		pages[i] += stream.String()
	}
	return invoicePDFWithFonts(fonts, pages...), nil
}

func invoicePDFWithFonts(fonts *invoiceFontRenderer, contents ...string) []byte {
	objects := []string{
		`<< /Type /Catalog /Pages 2 0 R >>`, "",
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>`,
	}
	resources := "/Regular 3 0 R /Bold 4 0 R /Mono 5 0 R "
	if fonts != nil {
		resources += fonts.appendFonts(&objects)
	}
	var kids strings.Builder
	for _, content := range contents {
		pageID, streamID := len(objects)+1, len(objects)+2
		fmt.Fprintf(&kids, "%d 0 R ", pageID)
		objects = append(objects, fmt.Sprintf(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << %s >> >> /Contents %d 0 R >>`, resources, streamID), invoiceStream(content))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(contents))
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return pdf.Bytes()
}

func invoicePDFLiteral(value string) string {
	return strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(invoiceASCII(value))
}

func invoiceASCII(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 32 && r <= 126 {
			return r
		}
		return ' '
	}, value)
}

func invoiceUTF16(value string) string {
	var raw []byte
	for _, r := range utf16.Encode([]rune(value)) {
		raw = append(raw, byte(r>>8), byte(r))
	}
	return hex.EncodeToString(raw)
}
