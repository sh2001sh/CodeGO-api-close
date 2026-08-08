package securityaudit

import (
	"fmt"
	"regexp"
)

const previewRuneLimit = 96

var previewSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)\b(?:api[_ -]?key|authorization|token|secret|password)\s*[:=]\s*[^\s,;]+`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
	regexp.MustCompile(`\b(?:\+?\d[\d -]{7,}\d)\b`),
}

func promptPreview(value string) string {
	redacted := value
	for _, pattern := range previewSensitivePatterns {
		redacted = pattern.ReplaceAllString(redacted, "[redacted]")
	}
	runes := []rune(redacted)
	truncated := len(runes) > previewRuneLimit
	if truncated {
		runes = runes[:previewRuneLimit]
	}
	if len(runes) == 0 {
		return ""
	}
	if truncated {
		return fmt.Sprintf("%s...", string(runes))
	}
	return string(runes)
}
