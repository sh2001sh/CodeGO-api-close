package bootstrap

import (
	"bytes"
	"os"
	"strings"

	defaultweb "github.com/sh2001sh/new-api/web/default"
)

func buildIndexPage() []byte {
	indexPage := defaultweb.DefaultIndexPage()
	indexPage = injectUmamiAnalytics(indexPage)
	indexPage = injectGoogleAnalytics(indexPage)
	return indexPage
}

func injectUmamiAnalytics(indexPage []byte) []byte {
	builder := &strings.Builder{}
	if os.Getenv("UMAMI_WEBSITE_ID") != "" {
		builder.WriteString("<script defer src=\"")
		scriptURL := os.Getenv("UMAMI_SCRIPT_URL")
		if scriptURL == "" {
			scriptURL = "https://analytics.umami.is/script.js"
		}
		builder.WriteString(scriptURL)
		builder.WriteString("\" data-website-id=\"")
		builder.WriteString(os.Getenv("UMAMI_WEBSITE_ID"))
		builder.WriteString("\"></script>")
	}
	builder.WriteString("<!--Umami QuantumNous-->\n")
	return bytes.ReplaceAll(indexPage, []byte("<!--umami-->\n"), []byte(builder.String()))
}

func injectGoogleAnalytics(indexPage []byte) []byte {
	builder := &strings.Builder{}
	if os.Getenv("GOOGLE_ANALYTICS_ID") != "" {
		gaID := os.Getenv("GOOGLE_ANALYTICS_ID")
		builder.WriteString("<script async src=\"https://www.googletagmanager.com/gtag/js?id=")
		builder.WriteString(gaID)
		builder.WriteString("\"></script>")
		builder.WriteString("<script>")
		builder.WriteString("window.dataLayer = window.dataLayer || [];")
		builder.WriteString("function gtag(){dataLayer.push(arguments);}")
		builder.WriteString("gtag('js', new Date());")
		builder.WriteString("gtag('config', '")
		builder.WriteString(gaID)
		builder.WriteString("');")
		builder.WriteString("</script>")
	}
	builder.WriteString("<!--Google Analytics QuantumNous-->\n")
	return bytes.ReplaceAll(indexPage, []byte("<!--Google Analytics-->\n"), []byte(builder.String()))
}
