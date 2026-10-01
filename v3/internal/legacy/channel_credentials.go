package legacy

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

func credentialProperties(secret string, info json.RawMessage, index int) (string, string, *time.Time, error) {
	kind, status := "api_key", "enabled"
	var source struct {
		Status map[string]int `json:"multi_key_status_list"`
	}
	if len(info) > 0 && string(info) != "null" {
		if err := json.Unmarshal(info, &source); err != nil {
			return "", "", nil, errors.New("legacy: invalid multikey status metadata")
		}
		if value, ok := source.Status[strconv.Itoa(index)]; ok && value != 1 {
			status = "disabled"
		}
	}
	var token struct {
		RefreshToken string          `json:"refresh_token"`
		ExpiresAt    json.RawMessage `json:"expires_at"`
		Expired      string          `json:"expired"`
		Expiry       string          `json:"expiry"`
	}
	if err := json.Unmarshal([]byte(secret), &token); err != nil || token.RefreshToken == "" {
		return kind, status, nil, nil
	}
	kind = "oauth"
	for _, candidate := range []string{token.Expired, token.Expiry} {
		if candidate != "" {
			if expiry, err := time.Parse(time.RFC3339, candidate); err == nil {
				return kind, status, &expiry, nil
			}
		}
	}
	var epoch int64
	if len(token.ExpiresAt) > 0 {
		if err := json.Unmarshal(token.ExpiresAt, &epoch); err == nil && epoch > 0 {
			if epoch > 100000000000 {
				epoch /= 1000
			}
			expiry := time.Unix(epoch, 0)
			return kind, status, &expiry, nil
		}
		var value string
		if err := json.Unmarshal(token.ExpiresAt, &value); err == nil {
			if expiry, err := time.Parse(time.RFC3339, value); err == nil {
				return kind, status, &expiry, nil
			}
		}
	}
	return "", "", nil, errors.New("legacy: OAuth credential requires a valid expiry")
}
