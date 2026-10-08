Invoice fonts are distributed under the SIL Open Font License 1.1 in OFL.txt.

- Noto Sans CJK Regular derives from NotoSansCJKjp-VF.otf at weight 400. Its Latin, Cyrillic, Chinese, Japanese and Korean glyphs are retained. Source: go-text/typesetting-utils revision 7d4869a3934e, opentype/common; upstream https://github.com/notofonts/noto-cjk.
- Noto Sans Arabic Regular is the unmodified NotoSansArabic.ttf from the same revision, including OpenType contextual shaping tables. Upstream https://github.com/notofonts/noto-fonts.

The PDF renderer embeds the used vector glyphs as searchable Type3 subsets rather than copying complete font files into each invoice. Rendering uses a Go HarfBuzz implementation and never fetches fonts at request time.
