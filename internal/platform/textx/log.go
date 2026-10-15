package textx

import "fmt"

const LocalLogContentLimit = 2048

// LocalLogPreview truncates long log-only content unless debug logging is enabled.
func LocalLogPreview(content string) string {
	if isDebugEnabled() || len(content) <= LocalLogContentLimit {
		return content
	}
	return fmt.Sprintf("%s... [truncated, original_length=%d, limit=%d]", content[:LocalLogContentLimit], len(content), LocalLogContentLimit)
}
