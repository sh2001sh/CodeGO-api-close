package dto

import "encoding/json"

// StripUnsupportedInputNamespaces removes non-standard namespace metadata from
// top-level Responses input items. Some clients replay this internal metadata
// in long conversations, while strict Responses-compatible upstreams reject it.
func (r *OpenAIResponsesRequest) StripUnsupportedInputNamespaces() (bool, error) {
	if r == nil || len(r.Input) == 0 {
		return false, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return false, nil
	}

	removed := false
	for index, item := range items {
		var inputItem map[string]json.RawMessage
		if err := json.Unmarshal(item, &inputItem); err != nil {
			continue
		}
		if _, found := inputItem["namespace"]; !found {
			continue
		}
		delete(inputItem, "namespace")
		normalizedItem, err := json.Marshal(inputItem)
		if err != nil {
			return false, err
		}
		items[index] = normalizedItem
		removed = true
	}
	if !removed {
		return false, nil
	}

	normalizedInput, err := json.Marshal(items)
	if err != nil {
		return false, err
	}
	r.Input = normalizedInput
	return true, nil
}
