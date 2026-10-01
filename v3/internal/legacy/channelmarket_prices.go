package legacy

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// The projection owns new JSON objects so the read-only source and every
// decimal token/cache price remain intact. Imported quota units round in pairs
// of micro-credits; new native owner prices keep their one-micro default.
func cmMarketPrices(source cmRow) (json.RawMessage, error) {
	raw, err := source.structured("model_prices", "{}")
	if err != nil {
		return nil, err
	}
	var models map[string]json.RawMessage
	if err = json.Unmarshal(raw, &models); err != nil || models == nil {
		return nil, errors.New("model prices must be an object")
	}
	for model, value := range models {
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(value, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("model price %s must be an object", model)
		}
		fields["money_quantum"] = json.RawMessage(`2`)
		if models[model], err = json.Marshal(fields); err != nil {
			return nil, err
		}
	}
	projected, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	if _, err = catalog.ParseMarketPrices(projected); err != nil {
		return nil, fmt.Errorf("source model prices do not match native channel pricing: %w", err)
	}
	return projected, nil
}
