package catalog

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ParseStatusCodeMapping accepts v2 numeric or quoted numeric values.
func ParseStatusCodeMapping(raw []byte) (map[string]int, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, fmt.Errorf("status mapping must be an object")
	}
	result := make(map[string]int, len(values))
	for source, raw := range values {
		from, err := strconv.Atoi(source)
		if err != nil || from < 100 || from > 599 {
			return nil, fmt.Errorf("invalid source status code")
		}
		text := string(raw)
		if len(text) > 0 && text[0] == '"' {
			if err := json.Unmarshal(raw, &text); err != nil {
				return nil, err
			}
		}
		to, err := strconv.Atoi(text)
		if err != nil || to < 100 || to > 599 {
			return nil, fmt.Errorf("invalid destination status code")
		}
		result[source] = to
	}
	return result, nil
}
