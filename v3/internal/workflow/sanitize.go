package workflow

import (
	"encoding/json"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func sanitizeResult(r native.Result, secret string) native.Result {
	if secret != "" {
		r.Error = strings.ReplaceAll(r.Error, secret, "[redacted]")
		r.URL = strings.ReplaceAll(r.URL, secret, "[redacted]")
	}
	var data any
	if len(r.Data) > 0 && json.Unmarshal(r.Data, &data) == nil {
		sanitize(data, secret)
		r.Data, _ = json.Marshal(data)
	}
	return r
}

func sanitize(v any, secret string) {
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			switch strings.ToLower(key) {
			case "api_key", "apikey", "authorization", "access_token", "refresh_token", "token", "access_key", "access_key_id", "secret_access_key", "client_secret", "secret", "secret_key", "credential", "credentials":
				delete(x, key)
				continue
			}
			if s, ok := value.(string); ok && secret != "" {
				x[key] = strings.ReplaceAll(s, secret, "[redacted]")
			} else {
				sanitize(value, secret)
			}
		}
	case []any:
		for i, value := range x {
			if s, ok := value.(string); ok && secret != "" {
				x[i] = strings.ReplaceAll(s, secret, "[redacted]")
			} else {
				sanitize(value, secret)
			}
		}
	}
}
