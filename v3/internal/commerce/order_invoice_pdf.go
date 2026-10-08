package commerce

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
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

// Invoices use PDF's built-in fonts and paginate long validated addresses.
// Chinese text uses the standard Adobe-GB1 CJK font instead of lossy ASCII.
func renderOrderInvoice(v orderInvoiceData) []byte {
	var stream strings.Builder
	var pages []string
	text := func(font string, size, x, y float64, value string) {
		if font == "CJK" {
			fmt.Fprintf(&stream, "BT /%s %.1f Tf %.1f %.1f Td <%s> Tj ET\n", font, size, x, y, invoiceUTF16(value))
			return
		}
		fmt.Fprintf(&stream, "BT /%s %.1f Tf %.1f %.1f Td (%s) Tj ET\n", font, size, x, y, invoicePDFLiteral(value))
	}
	line := func(y float64) {
		fmt.Fprintf(&stream, "0.88 0.88 0.87 RG 0.6 w 44 %.1f m 551 %.1f l S\n", y, y)
	}
	stream.WriteString("0.13 0.14 0.15 rg\n")
	stream.WriteString(invoiceLogoVector)
	text("Bold", 18, 87, 779, "CodeGo AI")
	text("Bold", 25, 400, 779, "INVOICE")
	text("Regular", 10, 44, 746, "CodeGo AI Limited")
	text("CJK", 10, 44, 729, "码高智能有限公司")
	stream.WriteString("0.93 0.95 0.93 rg 483 739 68 21 re f\n0.24 0.35 0.27 rg\n")
	text("Bold", 9, 503, 746, "PAID")
	stream.WriteString("0.13 0.14 0.15 rg\n")
	text("Regular", 9, 339, 725, "Invoice no.")
	text("Mono", 10, 339, 709, v.number)
	hkt := time.FixedZone("HKT", 28800)
	text("Regular", 9, 44, 701, "Issue date: "+v.issued.In(hkt).Format("02 Jan 2006 15:04 HKT"))
	text("Regular", 9, 339, 686, "Transaction date (Hong Kong time)")
	text("Regular", 10, 339, 670, v.paid.In(hkt).Format("02 Jan 2006 15:04 HKT"))
	y := 646.0
	// Addresses are never ellipsized. The validated 600-rune maximum fits in
	// eleven full-width lines even when every character is Chinese.
	for _, value := range invoiceWrap(v.sellerAddress, 112, 20) {
		text("CJK", 9, 44, y, value)
		y -= 12
	}
	y -= 12
	line(y)
	y -= 25
	text("Bold", 9, 44, y, "BILLED TO")
	y -= 21
	for _, value := range invoiceWrap(v.buyer, 92, 10) {
		text("CJK", 11, 44, y, value)
		y -= 15
	}
	for _, value := range invoiceWrap(v.buyerAddress, 112, 20) {
		text("CJK", 9, 44, y, value)
		y -= 12
	}
	y -= 12
	text("Regular", 8, 44, y, "Payment reference / "+invoiceASCII(v.provider))
	y -= 13
	for _, value := range invoiceWrap(v.trade, 112, 10) {
		text("CJK", 9, 44, y, value)
		y -= 12
	}
	y -= 18
	if y < 337 {
		text("Regular", 9, 44, y, "Items and totals continue on page 2.")
		text("Regular", 8, 44, 58, "CodeGo AI Limited / Hong Kong")
		text("Regular", 8, 512, 58, "1 / 2")
		pages = append(pages, stream.String())
		stream.Reset()
		stream.WriteString("0.13 0.14 0.15 rg\n")
		stream.WriteString(invoiceLogoVector)
		text("Bold", 18, 87, 779, "CodeGo AI")
		text("Bold", 25, 400, 779, "INVOICE")
		text("Mono", 10, 44, 746, v.number)
		line(725)
		y = 697
	}
	fmt.Fprintf(&stream, "0.96 0.96 0.95 rg 44 %.1f 507 29 re f\n0.13 0.14 0.15 rg\n", y-9)
	text("Bold", 9, 56, y+2, "DESCRIPTION")
	text("Bold", 9, 270, y+2, "QTY")
	text("Bold", 9, 348, y+2, "UNIT PRICE")
	text("Bold", 9, 491, y+2, "AMOUNT")
	y -= 30
	for i, value := range invoiceWrap(v.description, 38, 3) {
		text("CJK", 10, 56, y-float64(i)*15, value)
	}
	text("Regular", 10, 280, y, "1")
	amount := v.currency + " " + v.amount
	priceSize := 8.0
	if len(amount) > 22 {
		priceSize = 7.5
	}
	text("Mono", priceSize, 418-float64(len(amount))*priceSize*0.6, y, amount)
	text("Mono", priceSize, 540-float64(len(amount))*priceSize*0.6, y, amount)
	y -= 46
	line(y)
	y -= 24
	text("Regular", 10, 345, y, "Subtotal")
	text("Mono", 10, 551-float64(len(amount))*6, y, amount)
	y -= 24
	text("Regular", 9, 345, y, "VAT / GST")
	text("Regular", 9, 491, y, "Not charged")
	y -= 17
	line(y)
	y -= 23
	text("Bold", 12, 345, y, "Total paid")
	y -= 26
	text("Mono", 12, 551-float64(len(amount))*7.2, y, amount)
	y -= 27
	text("Regular", 10, 345, y, "Balance due")
	text("Mono", 10, 551-float64(len(v.currency+" 0"))*6, y, v.currency+" 0")
	text("Regular", 8, 44, 88, "Commercial invoice. Reimbursement requirements may differ by jurisdiction.")
	text("Regular", 8, 44, 74, "Electronically issued from the paid order. No VAT or GST charged.")
	text("Regular", 8, 44, 58, "CodeGo AI Limited / Hong Kong")
	pageNumber := "1 / 1"
	if len(pages) != 0 {
		pageNumber = "2 / 2"
	}
	text("Regular", 8, 512, 58, pageNumber)
	return invoicePDF(append(pages, stream.String())...)
}

func invoicePDF(contents ...string) []byte {
	content := contents[0]
	objects := []string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /Regular 4 0 R /Bold 5 0 R /Mono 6 0 R /CJK 7 0 R >> >> /Contents 10 0 R >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>`,
		`<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [8 0 R] /ToUnicode 11 0 R >>`,
		`<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> /FontDescriptor 9 0 R /DW 1000 /W [1 95 500] >>`,
		`<< /Type /FontDescriptor /FontName /STSong-Light /Flags 6 /FontBBox [-25 -254 1000 880] /ItalicAngle 0 /Ascent 880 /Descent -120 /CapHeight 880 /StemV 80 >>`,
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
	}
	const unicodeMap = `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /CodeGo-UTF16 def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfrange
<0000> <FFFF> <0000>
endbfrange
endcmap
CMapName currentdict /CMap defineresource pop
end
end
`
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(unicodeMap), unicodeMap))
	kids := "3 0 R"
	for _, next := range contents[1:] {
		pageID, streamID := len(objects)+1, len(objects)+2
		kids += fmt.Sprintf(" %d 0 R", pageID)
		objects = append(objects, fmt.Sprintf(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /Regular 4 0 R /Bold 5 0 R /Mono 6 0 R /CJK 7 0 R >> >> /Contents %d 0 R >>`, streamID),
			fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(next), next))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(contents))
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

func invoiceWrap(value string, width, maxLines int) []string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	var lines []string
	var line strings.Builder
	units := 0
	for _, r := range value {
		next := 1
		if r > 127 {
			next = 2
		}
		if units+next > width {
			lines = append(lines, line.String())
			line.Reset()
			units = 0
			if len(lines) == maxLines {
				last := []rune(lines[maxLines-1])
				lines[maxLines-1] = string(last[:len(last)-1]) + "…"
				return lines
			}
		}
		line.WriteRune(r)
		units += next
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return lines
}
