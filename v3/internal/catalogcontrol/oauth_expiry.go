package catalogcontrol

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Existing Codex exports carry `expired` as RFC3339. New token documents
// normally carry expires_at in Unix seconds or expiry_date in milliseconds.
func oauthExpiry(secret string) *time.Time {
	var document map[string]json.RawMessage
	if json.Unmarshal([]byte(secret), &document) != nil {
		return nil
	}
	for _, key := range []string{"expired", "expires_at", "expiry_date"} {
		raw := document[key]
		if len(raw) == 0 {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) != nil {
			text = string(raw)
		}
		if value, err := time.Parse(time.RFC3339, text); err == nil {
			return &value
		}
		if count, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil && count > 0 {
			value := time.Unix(count, 0).UTC()
			if key == "expiry_date" || count >= 1000000000000 {
				value = time.UnixMilli(count).UTC()
			}
			return &value
		}
	}
	return nil
}
