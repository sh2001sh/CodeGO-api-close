package bedrock

import "strings"

// These aliases are the public model names accepted by the v2 AWS adapter.
var modelAliases = map[string]string{
	"claude-3-sonnet-20240229":   "anthropic.claude-3-sonnet-20240229-v1:0",
	"claude-3-opus-20240229":     "anthropic.claude-3-opus-20240229-v1:0",
	"claude-3-haiku-20240307":    "anthropic.claude-3-haiku-20240307-v1:0",
	"claude-3-5-sonnet-20240620": "anthropic.claude-3-5-sonnet-20240620-v1:0",
	"claude-3-5-sonnet-20241022": "anthropic.claude-3-5-sonnet-20241022-v2:0",
	"claude-3-5-haiku-20241022":  "anthropic.claude-3-5-haiku-20241022-v1:0",
	"claude-3-7-sonnet-20250219": "anthropic.claude-3-7-sonnet-20250219-v1:0",
	"claude-sonnet-4-20250514":   "anthropic.claude-sonnet-4-20250514-v1:0",
	"claude-opus-4-20250514":     "anthropic.claude-opus-4-20250514-v1:0",
	"claude-opus-4-1-20250805":   "anthropic.claude-opus-4-1-20250805-v1:0",
	"claude-sonnet-4-5-20250929": "anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-6":          "anthropic.claude-sonnet-4-6",
	"claude-haiku-4-5-20251001":  "anthropic.claude-haiku-4-5-20251001-v1:0",
	"claude-opus-4-5-20251101":   "anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-6":            "anthropic.claude-opus-4-6-v1",
	"claude-opus-4-7":            "anthropic.claude-opus-4-7",
	"claude-opus-4-8":            "anthropic.claude-opus-4-8",
	"nova-micro-v1:0":            "amazon.nova-micro-v1:0",
	"nova-lite-v1:0":             "amazon.nova-lite-v1:0",
	"nova-pro-v1:0":              "amazon.nova-pro-v1:0",
	"nova-premier-v1:0":          "amazon.nova-premier-v1:0",
}

func modelID(model, region string) string {
	mapped, alias := modelAliases[model]
	if !alias {
		// Explicit Bedrock IDs, ARNs and inference profiles are authoritative.
		return model
	}
	prefix, _, _ := strings.Cut(region, "-")
	if prefix != "us" && prefix != "eu" && prefix != "ap" {
		return mapped
	}
	usOnly := strings.Contains(model, "claude-3-opus-") || strings.Contains(model, "claude-3-5-haiku-") || model == "claude-opus-4-20250514" || model == "claude-opus-4-1-20250805" || model == "nova-premier-v1:0"
	if usOnly && prefix != "us" {
		return mapped
	}
	if model == "claude-3-5-sonnet-20241022" && prefix == "eu" {
		return mapped
	}
	if prefix == "ap" {
		prefix = "apac"
	}
	return prefix + "." + mapped
}
