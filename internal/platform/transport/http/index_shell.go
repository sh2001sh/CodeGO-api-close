package http

import (
	"regexp"
	"strings"
)

var (
	htmlTagPattern   = regexp.MustCompile(`(?is)<html\b[^>]*>`)
	titlePattern     = regexp.MustCompile(`(?is)<title>.*?</title>`)
	h1Pattern        = regexp.MustCompile(`(?is)<h1\b[^>]*>.*?</h1>`)
	bodyOpenPattern  = regexp.MustCompile(`(?is)<body\b[^>]*>`)
	headClosePattern = regexp.MustCompile(`(?is)</head>`)
)

// MergeIndexShell keeps built asset references while refreshing SEO shell content.
func MergeIndexShell(builtPage, sourceTemplate []byte) []byte {
	built := string(builtPage)
	source := string(sourceTemplate)

	built = replaceHTMLTag(built, source)
	built = replaceTitle(built, source)

	for _, name := range []string{"title", "description", "keywords"} {
		built = replaceMetaByName(built, source, name)
	}

	built = replaceOrInsertH1(built, source)
	return []byte(built)
}

func replaceHTMLTag(target, source string) string {
	tag := htmlTagPattern.FindString(source)
	if tag == "" {
		return target
	}
	if htmlTagPattern.MatchString(target) {
		return htmlTagPattern.ReplaceAllString(target, tag)
	}
	return target
}

func replaceTitle(target, source string) string {
	title := titlePattern.FindString(source)
	if title == "" {
		return target
	}
	if titlePattern.MatchString(target) {
		return titlePattern.ReplaceAllString(target, title)
	}
	return insertBeforeHeadClose(target, title)
}

func replaceMetaByName(target, source, name string) string {
	metaPattern := regexp.MustCompile(`(?is)<meta\b[^>]*name=["']` + regexp.QuoteMeta(name) + `["'][^>]*>`)
	meta := metaPattern.FindString(source)
	if meta == "" {
		return target
	}
	if metaPattern.MatchString(target) {
		return metaPattern.ReplaceAllString(target, meta)
	}
	return insertBeforeHeadClose(target, meta)
}

func replaceOrInsertH1(target, source string) string {
	h1 := h1Pattern.FindString(source)
	if h1 == "" {
		return target
	}
	if h1Pattern.MatchString(target) {
		return h1Pattern.ReplaceAllString(target, h1)
	}
	bodyTag := bodyOpenPattern.FindString(target)
	if bodyTag == "" {
		return target
	}
	replacement := bodyTag + "\n    " + indentBlock(h1, "    ")
	return bodyOpenPattern.ReplaceAllString(target, replacement)
}

func insertBeforeHeadClose(target, snippet string) string {
	if !headClosePattern.MatchString(target) {
		return target
	}
	return headClosePattern.ReplaceAllString(target, "    "+indentBlock(snippet, "    ")+"\n  </head>")
}

func indentBlock(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
