package pricing

import (
	"fmt"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// These are private admitted price facts, not new published configuration.
// Both survive JSON task restore as exact integers/json.Number.
func frozenVideoFacts(p catalog.Price) (duration, ppm int64, err error) {
	ppm = 1_000_000
	for _, key := range []string{"veo_duration_micros", "veo_resolution_multiplier_ppm"} {
		value, exists := p.Rules[key]
		if !exists {
			continue
		}
		if p.Mode != "per_request" || p.Rules["billing_unit"] != "video_second" {
			return 0, 0, fmt.Errorf("pricing: frozen Veo facts require video_second")
		}
		n, e := catalogNumber(value)
		if e != nil || !n.IsInt() || !n.Num().IsInt64() || n.Sign() <= 0 {
			return 0, 0, fmt.Errorf("pricing: invalid frozen %s", key)
		}
		if key == "veo_duration_micros" {
			duration = n.Num().Int64()
		} else {
			ppm = n.Num().Int64()
		}
	}
	return duration, ppm, nil
}
