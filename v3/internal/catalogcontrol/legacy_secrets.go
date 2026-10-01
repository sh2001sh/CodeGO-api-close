package catalogcontrol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// v2 supports line keys and Vertex credential JSON arrays. Structured
// credentials are retained intact; their fields never become error text.
func legacySecrets(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		var values []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &values); err != nil || len(values) > 10000 {
			return nil, fmt.Errorf("invalid credential array")
		}
		secrets := make([]string, 0, len(values))
		for _, value := range values {
			secret := string(value)
			if len(value) == 0 || (value[0] != '{' && value[0] != '"') {
				return nil, fmt.Errorf("credential arrays must contain strings or objects")
			}
			if value[0] == '"' {
				if json.Unmarshal(value, &secret) != nil {
					return nil, fmt.Errorf("invalid credential string")
				}
			}
			if secret = strings.TrimSpace(secret); secret != "" {
				secrets = append(secrets, secret)
			}
		}
		return secrets, nil
	}
	if json.Valid([]byte(raw)) {
		if strings.HasPrefix(raw, "\"") {
			if json.Unmarshal([]byte(raw), &raw) != nil {
				return nil, fmt.Errorf("invalid credential string")
			}
		}
		return []string{raw}, nil
	}
	return strings.Split(raw, "\n"), nil
}
