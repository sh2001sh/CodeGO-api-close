package legacy

import (
	"encoding/json"
	"strings"
)

// completionRatio reproduces v2's locked completion-rate precedence. A stored
// override only wins for unlocked models or explicitly namespaced models.
func completionRatio(model string, overrides map[string]json.Number) string {
	if strings.Contains(model, "/") {
		if value, ok := overrides[model]; ok {
			return value.String()
		}
	}
	ratio, locked := defaultCompletion(model)
	if locked {
		return ratio
	}
	if value, ok := overrides[model]; ok {
		return value.String()
	}
	return ratio
}

func defaultCompletion(model string) (string, bool) {
	if strings.HasSuffix(model, "-all") || strings.HasSuffix(model, "-gizmo-*") {
		return "2", false
	}
	if strings.HasPrefix(model, "gpt-") {
		if strings.HasPrefix(model, "gpt-4o") {
			if model == "gpt-4o-2024-05-13" {
				return "3", true
			}
			if strings.HasPrefix(model, "gpt-4o-mini-tts") {
				return "20", false
			}
			return "4", false
		}
		if strings.HasPrefix(model, "gpt-5") {
			if strings.HasPrefix(model, "gpt-5.6") || strings.HasPrefix(model, "gpt-5.5") {
				return "6", true
			}
			if strings.HasPrefix(model, "gpt-5.4") {
				if strings.HasPrefix(model, "gpt-5.4-nano") {
					return "6.25", true
				}
				return "6", true
			}
			return "8", true
		}
		if strings.HasPrefix(model, "gpt-4.5-preview") {
			return "2", true
		}
		if strings.HasPrefix(model, "gpt-4-turbo") || strings.HasSuffix(model, "gpt-4-1106") || strings.HasSuffix(model, "gpt-4-1105") {
			return "3", true
		}
		return "2", false
	}
	if strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") {
		return "4", true
	}
	if model == "chatgpt-4o-latest" {
		return "3", true
	}
	if strings.Contains(model, "claude-3") || strings.Contains(model, "claude-sonnet-4") || strings.Contains(model, "claude-opus-4") || strings.Contains(model, "claude-haiku-4") {
		return "5", true
	}
	if strings.HasPrefix(model, "mistral-") {
		return "3", true
	}
	if strings.HasPrefix(model, "gemini-") {
		if strings.HasPrefix(model, "gemini-1.5") || strings.HasPrefix(model, "gemini-2.0") {
			return "4", true
		}
		if strings.HasPrefix(model, "gemini-2.5-pro") {
			return "8", false
		}
		if strings.HasPrefix(model, "gemini-2.5-flash-preview") {
			if strings.HasSuffix(model, "-nothinking") {
				return "4", false
			}
			return "70/3", false
		}
		if strings.HasPrefix(model, "gemini-2.5-flash-lite") {
			return "4", false
		}
		if strings.HasPrefix(model, "gemini-2.5-flash") || strings.HasPrefix(model, "gemini-robotics-er-1.5") {
			return "25/3", false
		}
		if strings.HasPrefix(model, "gemini-3-pro-image") {
			return "60", false
		}
		if strings.HasPrefix(model, "gemini-3-pro") {
			return "6", false
		}
		return "4", false
	}
	if strings.HasPrefix(model, "command") {
		switch model {
		case "command-r":
			return "3", true
		case "command-r-plus":
			return "5", true
		case "command-r-08-2024", "command-r-plus-08-2024":
			return "4", true
		default:
			return "4", false
		}
	}
	for _, prefix := range []string{"ERNIE-Speed-", "ERNIE-Lite-", "ERNIE-Character", "ERNIE-Functions"} {
		if strings.HasPrefix(model, prefix) {
			return "2", true
		}
	}
	switch model {
	case "llama2-70b-4096":
		return "5/4", true
	case "llama3-8b-8192":
		return "2", true
	case "llama3-70b-8192":
		return "79/59", true
	}
	return "1", false
}
