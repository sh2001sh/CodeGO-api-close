package app

import (
	"strconv"
	"strings"
)

var marketplaceSourcePrefixes = map[string]string{
	"Codex Plus": "Codex-Plus",
	"Codex Pro":  "Codex-Pro",
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
	label, ok := canonicalSourceLabel(sourceLabel)
	if !ok {
		label = "来源待审核"
	}
	multiplierText := strconv.FormatFloat(multiplier, 'f', -1, 64)
	return label + "-" + multiplierText + "x-" + strings.TrimSpace(channelID)
}
