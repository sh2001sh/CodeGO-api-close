package commerce

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/bidi"
)

// The source fonts are bundled under SIL OFL 1.1. PDFs embed only the used
// vector glyphs as Type3 subsets, so readers need neither installed fonts nor
// network access. ActualText preserves logical Unicode, including RTL text.
//
//go:embed invoicefonts/NotoSansCJK-Regular.otf
var invoiceCJKFont []byte

//go:embed invoicefonts/NotoSansArabic-Regular.ttf
var invoiceArabicFont []byte

var invoiceSourceFonts = sync.OnceValue(func() []*font.Font {
	var sources []*font.Font
	for _, data := range [][]byte{invoiceCJKFont, invoiceArabicFont} {
		face, err := font.ParseTTF(bytes.NewReader(data))
		if err != nil {
			// These immutable, compiled-in assets are verified by renderer tests.
			panic(fmt.Errorf("invalid embedded invoice font: %w", err))
		}
		sources = append(sources, face.Font)
	}
	return sources
})

type invoiceFontMap []*font.Face

func (faces invoiceFontMap) ResolveFace(r rune) *font.Face {
	for _, face := range faces {
		if _, ok := face.NominalGlyph(r); ok {
			return face
		}
	}
	return faces[0]
}

type invoiceGlyphKey struct {
	face *font.Face
	id   font.GID
}

type invoiceGlyph struct {
	key   invoiceGlyphKey
	width float64
	text  string
}

type invoiceGlyphCode struct{ subset, code int }

type invoiceFontRenderer struct {
	faces   invoiceFontMap
	shaper  shaping.HarfbuzzShaper
	segment shaping.Segmenter
	codes   map[invoiceGlyphKey]invoiceGlyphCode
	subsets [][]invoiceGlyph
}

func newInvoiceFontRenderer() *invoiceFontRenderer {
	r := &invoiceFontRenderer{codes: make(map[invoiceGlyphKey]invoiceGlyphCode)}
	// Face caches are mutable. Every document owns its own faces and shaper;
	// only the immutable parsed Font data is shared between concurrent requests.
	for _, source := range invoiceSourceFonts() {
		r.faces = append(r.faces, font.NewFace(source))
	}
	return r
}

func (r *invoiceFontRenderer) validate(value string) error {
	for _, ch := range value {
		// Addresses allow newlines; controls in provider metadata are escaped
		// by the literal writer. OpenType shaping handles join controls.
		if unicode.IsControl(ch) || ch == '\u200c' || ch == '\u200d' {
			continue
		}
		if id, ok := r.faces.ResolveFace(ch).NominalGlyph(ch); !ok || id == 0 {
			return fmt.Errorf("%w: invoice font does not support U+%04X", ErrInvalid, ch)
		}
	}
	return nil
}

func (r *invoiceFontRenderer) runs(value string, size float64) []shaping.Output {
	text := []rune(value)
	direction := di.DirectionLTR
	for _, ch := range text {
		properties, _ := bidi.LookupRune(ch)
		if properties.Class() == bidi.R || properties.Class() == bidi.AL {
			direction = di.DirectionRTL
			break
		}
		if properties.Class() == bidi.L {
			break
		}
	}
	inputs := r.segment.Split(shaping.Input{Text: text, RunEnd: len(text), Direction: direction,
		Size: fixed.Int26_6(size * 64)}, r.faces)
	outputs := make([]shaping.Output, len(inputs))
	for i, input := range inputs {
		outputs[i] = r.shaper.Shape(input)
		position := i
		if direction == di.DirectionRTL {
			position = len(inputs) - 1 - i
		}
		outputs[i].VisualIndex = int32(position)
	}
	// Use the shaper's line ordering with the Unicode first-strong paragraph
	// direction. Opposite-direction runs may be split by script or font.
	for i := 0; i < len(outputs); {
		if outputs[i].Direction == direction {
			i++
			continue
		}
		end := i + 1
		for end < len(outputs) && outputs[end].Direction != direction {
			end++
		}
		for left, right := i, end-1; left < right; left, right = left+1, right-1 {
			outputs[left].VisualIndex, outputs[right].VisualIndex = outputs[right].VisualIndex, outputs[left].VisualIndex
		}
		i = end
	}
	sort.SliceStable(outputs, func(i, j int) bool { return outputs[i].VisualIndex < outputs[j].VisualIndex })
	return outputs
}

func (r *invoiceFontRenderer) text(stream *strings.Builder, size, x, y float64, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(stream, "/Span << /ActualText <feff%s> >> BDC\n", invoiceUTF16(value))
	text := []rune(value)
	for _, run := range r.runs(value, size) {
		for _, glyph := range run.Glyphs {
			key := invoiceGlyphKey{run.Face, glyph.GlyphID}
			code, ok := r.codes[key]
			if !ok {
				if len(r.subsets) == 0 || len(r.subsets[len(r.subsets)-1]) == 255 {
					r.subsets = append(r.subsets, nil)
				}
				code = invoiceGlyphCode{len(r.subsets) - 1, len(r.subsets[len(r.subsets)-1]) + 1}
				cluster := " "
				if index := glyph.TextIndex(); index >= 0 && index < len(text) {
					end := min(len(text), index+glyph.RunesCount())
					cluster = string(text[index:end])
				}
				r.subsets[code.subset] = append(r.subsets[code.subset], invoiceGlyph{key,
					float64(glyph.Advance) / 64 / size * 1000, cluster})
				r.codes[key] = code
			}
			fmt.Fprintf(stream, "BT /UF%d %.2f Tf 1 0 0 1 %.3f %.3f Tm <%02x> Tj ET\n",
				code.subset, size, x+float64(glyph.XOffset)/64, y+float64(glyph.YOffset)/64, code.code)
			x += float64(glyph.Advance) / 64
		}
	}
	stream.WriteString("EMC\n")
}

func (r *invoiceFontRenderer) width(value string, size float64) float64 {
	var width float64
	for _, run := range r.runs(value, size) {
		width += float64(run.Advance) / 64
	}
	return width
}

// wrap measures shaped glyphs instead of counting code points. It preserves
// every rune, honors newlines, and never splits a combining-mark cluster.
func (r *invoiceFontRenderer) wrap(value string, size, width float64) []string {
	var lines []string
	for _, paragraph := range strings.Split(value, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		text := []rune(paragraph)
		start := 0
		for start < len(text) {
			end := start + 1
			for end <= len(text) && r.width(string(text[start:end]), size) <= width {
				end++
			}
			end = max(start+1, end-1)
			// Prefer a word boundary when it still uses at least half the line;
			// keep the separator so the printed content remains complete.
			if end < len(text) {
				for boundary := end - 1; boundary > start+(end-start)/2; boundary-- {
					if unicode.IsSpace(text[boundary]) {
						end = boundary + 1
						break
					}
				}
			}
			// Combining marks and join controls belong to the preceding glyph.
			for end < len(text) && (text[end] == '\u200d' || text[end] == '\u200c' || invoiceCombining(text[end])) {
				end++
			}
			lines = append(lines, string(text[start:end]))
			start = end
		}
	}
	return lines
}

func (r *invoiceFontRenderer) appendFonts(objects *[]string) string {
	var resources strings.Builder
	for subset, glyphs := range r.subsets {
		fontID := len(*objects) + 1
		*objects = append(*objects, "")
		var procedures, widths, encoding, mapping strings.Builder
		for i, glyph := range glyphs {
			id := len(*objects) + 1
			*objects = append(*objects, invoiceStream(invoiceGlyphOutline(glyph)))
			fmt.Fprintf(&procedures, "/g%d %d 0 R ", i+1, id)
			fmt.Fprintf(&widths, "%.3f ", glyph.width)
			fmt.Fprintf(&encoding, "/g%d ", i+1)
			if i%100 == 0 {
				fmt.Fprintf(&mapping, "%d beginbfchar\n", min(100, len(glyphs)-i))
			}
			fmt.Fprintf(&mapping, "<%02x> <%s>\n", i+1, invoiceUTF16(glyph.text))
			if i%100 == 99 || i == len(glyphs)-1 {
				mapping.WriteString("endbfchar\n")
			}
		}
		toUnicode := fmt.Sprintf("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /CodeGoSubset%d def\n/CMapType 2 def\n1 begincodespacerange\n<00> <ff>\nendcodespacerange\n%sendcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n", subset, mapping.String())
		mapID := len(*objects) + 1
		*objects = append(*objects, invoiceStream(toUnicode))
		(*objects)[fontID-1] = fmt.Sprintf("<< /Type /Font /Subtype /Type3 /Name /UF%d /FontBBox [-2000 -2000 5000 5000] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << %s >> /Encoding << /Type /Encoding /Differences [1 %s] >> /FirstChar 1 /LastChar %d /Widths [%s] /Resources << >> /ToUnicode %d 0 R >>", subset, procedures.String(), encoding.String(), len(glyphs), widths.String(), mapID)
		fmt.Fprintf(&resources, "/UF%d %d 0 R ", subset, fontID)
	}
	return resources.String()
}

func invoiceGlyphOutline(glyph invoiceGlyph) string {
	var stream strings.Builder
	fmt.Fprintf(&stream, "%.3f 0 d0\n", glyph.width)
	outline, ok := glyph.key.face.GlyphData(glyph.key.id).(font.GlyphOutline)
	if !ok {
		return stream.String()
	}
	scale := float32(1000) / float32(glyph.key.face.Upem())
	var previous opentype.SegmentPoint
	for _, segment := range outline.Segments {
		args := segment.Args
		for i := range args {
			args[i].X *= scale
			args[i].Y *= scale
		}
		switch segment.Op {
		case opentype.SegmentOpMoveTo:
			fmt.Fprintf(&stream, "%.3f %.3f m\n", args[0].X, args[0].Y)
		case opentype.SegmentOpLineTo:
			fmt.Fprintf(&stream, "%.3f %.3f l\n", args[0].X, args[0].Y)
		case opentype.SegmentOpQuadTo:
			fmt.Fprintf(&stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n",
				previous.X+(args[0].X-previous.X)*2/3, previous.Y+(args[0].Y-previous.Y)*2/3,
				args[1].X+(args[0].X-args[1].X)*2/3, args[1].Y+(args[0].Y-args[1].Y)*2/3, args[1].X, args[1].Y)
		case opentype.SegmentOpCubeTo:
			fmt.Fprintf(&stream, "%.3f %.3f %.3f %.3f %.3f %.3f c\n", args[0].X, args[0].Y, args[1].X, args[1].Y, args[2].X, args[2].Y)
		}
		previous = args[len(segment.ArgsSlice())-1]
	}
	stream.WriteString("f\n")
	return stream.String()
}

func invoiceStream(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)
}

func invoiceCombining(r rune) bool { return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) }
