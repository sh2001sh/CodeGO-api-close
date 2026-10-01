package gateway

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// Metadata describes a reachable model; its own status is not a channel ACL.
// Keep the public model ID, rather than returning a prefix or suffix pattern.
func enrichDiscoveryMetadata(item *discoveryModel, metadata catalog.MetadataSnapshot) {
	model, vendor := metadata.Describe(item.ID)
	if model == nil {
		return
	}
	item.Description, item.Icon, item.Tags = model.Description, model.Icon, model.Tags
	if vendor != nil && strings.TrimSpace(vendor.Name) != "" {
		item.OwnedBy = vendor.Name
	}
	item.SupportedEndpointTypes, item.generationMethods = discoveryEndpoints(model.Endpoints)
}

// V2 accepts endpoint objects whose values are paths or {path,method} objects.
// Preserve those exact keys. Opaque legacy text and arbitrary object fields
// never become a claimed native Google capability or a public raw JSON blob.
func discoveryEndpoints(raw string) ([]string, []string) {
	var entries map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &entries) != nil {
		return nil, nil
	}
	var types, methods []string
	for key, entry := range entries {
		var path string
		method := "POST"
		if json.Unmarshal(entry, &path) != nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(entry, &fields) != nil || fields == nil {
				continue
			}
			if pathField := fields["path"]; pathField != nil && json.Unmarshal(pathField, &path) != nil {
				continue
			}
			if methodField := fields["method"]; methodField != nil && json.Unmarshal(methodField, &method) != nil {
				continue
			}
		}
		if string(entry) == "null" {
			continue
		}
		types = append(types, key)
		if action := discoveryGoogleMethod(path, method); action != "" {
			methods = append(methods, action)
		}
	}
	slices.Sort(types)
	slices.Sort(methods)
	return types, slices.Compact(methods)
}

func discoveryGoogleMethod(path, method string) string {
	if strings.ToUpper(strings.TrimSpace(method)) != "POST" ||
		(!strings.HasPrefix(path, "/v1beta/models/") && !strings.HasPrefix(path, "/v1/models/")) {
		return ""
	}
	_, action, found := strings.Cut(path, ":")
	if !found {
		return ""
	}
	switch action {
	case "generateContent", "streamGenerateContent", "embedContent", "batchEmbedContents", "countTokens":
		return action
	default:
		return ""
	}
}
