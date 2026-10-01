package legacy

import (
	_ "embed"
	"encoding/json"
)

// Frozen v2 built-ins are required when an option has never been persisted.
// Rational strings preserve constants such as USD / USD2RMB exactly; these
// migration defaults are independent of, and do not import, the v2 binary.
//
//go:embed pricing_defaults.json
var pricingDefaultsJSON []byte

func pricingDefaults() (map[string]map[string]string, error) {
	var values map[string]map[string]string
	err := json.Unmarshal(pricingDefaultsJSON, &values)
	return values, err
}
