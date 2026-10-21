package app

import (
	"regexp"
	"strings"
	"unicode"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
)

var sourceLinkPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://|(?:https?|hxxps?|mailto|javascript|data):|www\.|(?:[\p{L}\p{N}][\p{L}\p{N}-]*\.)+(?:[\p{L}]{2,}|xn--[a-z0-9-]+)|[0-9]{1,3}(?:\.[0-9]{1,3}){3}|(?:[0-9a-f]{0,4}:){2,}[0-9a-f]{0,4})`)

func containsSourceLink(label string) bool {
	var compact strings.Builder
	for _, char := range label {
		if unicode.IsLetter(char) || unicode.IsNumber(char) || strings.ContainsRune(".:@/+-", char) {
			compact.WriteRune(char)
		}
	}
	return sourceLinkPattern.MatchString(compact.String())
}

var marketplaceSourcePrefixes = map[string]string{
	"Codex Plus": "Codex-Plus",
	"Codex Pro":  "Codex-Pro",
	"Grok":       "Grok",
	"Gemini":     "Gemini",
	"Codex 混合号池": "Codex-Mixed",
	"CC-Max":     "CC-Max",
	"CC-Kiro":    "CC-Kiro",
	"CC其它":       "CC-Other",
	"国产模型":       "CN-Model",
}

func canonicalSourceLabel(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for label := range marketplaceSourcePrefixes {
		if strings.EqualFold(label, value) {
			return label, true
		}
	}
	switch strings.ToLower(value) {
	case "plus", "codex-plus":
		return "Codex Plus", true
	case "pro", "codex-pro":
		return "Codex Pro", true
	case "codex mixed", "codex-mixed", "mixed", "codex 混合号池":
		return "Codex 混合号池", true
	case "claude max", "claudemax", "cc max":
		return "CC-Max", true
	case "kiro", "cc kiro":
		return "CC-Kiro", true
	case "claude", "cc other", "cc-other":
		return "CC其它", true
	case "domestic", "cn model", "cn-model":
		return "国产模型", true
	default:
		return "", false
	}
}

func marketplaceInternalGroupName(sourceLabel, groupID string) string {
	label, ok := canonicalSourceLabel(sourceLabel)
	if !ok {
		label = "CC其它"
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(groupID), "-", ""))
	if len(compact) > 6 {
		compact = compact[:6]
	}
	if compact == "" {
		compact = "group"
	}
	return marketplaceSourcePrefixes[label] + "-" + compact
}

func marketplaceDisplayName(sourceLabel string, multiplier float64, channelID string) string {
	label := strings.TrimSpace(sourceLabel)
	if label == "" {
		label = "未声明来源"
	}
	multiplierText := marketplacedomain.FormatMultiplier(multiplier)
	return strings.TrimSpace(channelID) + "-" + label + "-" + multiplierText + "x"
}
