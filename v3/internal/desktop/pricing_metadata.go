package desktop

import (
	"encoding/json"
	"slices"
)

type endpointInfo struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}

func pricingEndpoints(raw string) ([]string, map[string]endpointInfo) {
	types := []string{}
	infos := map[string]endpointInfo{}
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &values) != nil {
		// Array declarations are also retained by existing model import files.
		_ = json.Unmarshal([]byte(raw), &types)
		return types, infos
	}
	for key, value := range values {
		if string(value) == "null" {
			continue
		}
		info := endpointInfo{Method: "POST"}
		if json.Unmarshal(value, &info.Path) != nil && json.Unmarshal(value, &info) != nil {
			continue
		}
		if info.Path == "" {
			continue
		}
		types = append(types, key)
		infos[key] = info
	}
	slices.Sort(types)
	return types, infos
}
