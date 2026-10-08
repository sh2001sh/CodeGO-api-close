package channelmarket

import "strings"

var groupTags = map[string]bool{
	"openai": true, "anthropic": true, "google": true, "deepseek": true,
	"xai": true, "meta": true, "mistral": true, "qwen": true,
	"moonshot": true, "zhipu": true, "cohere": true,
}

// Legacy use-case tags do not imply any provider. Hide them without inventing
// a mapping, while retaining valid self-declared providers from mixed records.
func visibleProviderTags(tags []string) []string {
	result := make([]string, 0, 5)
	seen := make(map[string]bool)
	for _, tag := range tags {
		if groupTags[tag] && !seen[tag] && len(result) < 5 {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	return result
}

func normalizeGroupTags(tags []string) ([]string, error) {
	result := make([]string, 0, len(groupTags))
	seen := make(map[string]bool)
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if !groupTags[tag] {
			return nil, ErrInvalid
		}
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	if len(result) > 5 {
		return nil, ErrInvalid
	}
	return result, nil
}
