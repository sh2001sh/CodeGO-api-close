package vertex

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func invalidRegionConfig() error {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error",
		Code: "invalid_channel_config", Message: "vertex: invalid channel region configuration"}
}

// configuredRegion preserves v2 Channel.Other/ApiVersion semantics. A present
// api_version is authoritative, including empty strings/maps resolving global;
// credential region fields remain a fallback when channel metadata is absent.
func configuredRegion(settings map[string]any, c Credentials, clientModel string) (string, error) {
	value, present := settings["api_version"]
	if !present || value == nil {
		return c.region(clientModel), nil
	}
	raw, ok := value.(string)
	if !ok {
		return "", invalidRegionConfig()
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		var values map[string]any
		if json.Unmarshal([]byte(raw), &values) != nil || values == nil {
			return "", invalidRegionConfig()
		}
		regions := make(map[string]string, len(values))
		for model, value := range values {
			region, ok := value.(string)
			if !ok {
				return "", invalidRegionConfig()
			}
			region, err := normalizeConfiguredRegion(region)
			if err != nil {
				return "", err
			}
			regions[model] = region
		}
		if region, ok := regions[clientModel]; ok {
			return region, nil
		}
		if region, ok := regions["default"]; ok {
			return region, nil
		}
		return "global", nil
	}
	return normalizeConfiguredRegion(raw)
}

func normalizeConfiguredRegion(region string) (string, error) {
	region = strings.TrimSpace(region)
	if region == "" || region == "global" {
		return "global", nil
	}
	// Regional endpoint labels are lowercase Google location IDs. Do not allow
	// path/query delimiters or host substitutions from channel configuration.
	if !strings.Contains(region, "-") || region[0] < 'a' || region[0] > 'z' || region[len(region)-1] == '-' {
		return "", invalidRegionConfig()
	}
	for _, r := range region {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return "", invalidRegionConfig()
		}
	}
	return region, nil
}
