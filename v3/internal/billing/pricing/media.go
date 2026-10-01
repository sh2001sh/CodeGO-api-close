package pricing

import (
	"fmt"
	"math/big"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const microsPerSecond int64 = 1_000_000

// MediaUnit returns the configured non-token billing dimension. Ordinary
// per-request prices remain per call, regardless of any reported media units.
func MediaUnit(p catalog.Price) (string, error) {
	if err := validateMediaPrice(p); err != nil {
		return "", err
	}
	unit, _ := p.Rules["billing_unit"].(string)
	return unit, nil
}

func validateMediaPrice(p catalog.Price) error {
	if _, _, err := frozenVideoFacts(p); err != nil {
		return err
	}
	value, configured := p.Rules["billing_unit"]
	rounding, rounded := p.Rules["unit_rounding"]
	if !configured {
		if rounded {
			return fmt.Errorf("pricing: unit_rounding requires billing_unit")
		}
		return nil
	}
	unit, ok := value.(string)
	if !ok || p.Mode != "per_request" || p.PerRequest < 0 {
		return fmt.Errorf("pricing: media billing requires per_request and a nonnegative unit price")
	}
	switch unit {
	case "image", "audio_character", "audio_second", "video_second":
	default:
		return fmt.Errorf("pricing: unknown billing_unit %q", unit)
	}
	if rounded && (rounding != "ceil_second" || unit != "audio_second" && unit != "video_second") {
		return fmt.Errorf("pricing: ceil_second is only supported for duration units")
	}
	return nil
}

func mediaAmount(u gateway.Usage, p catalog.Price) (*big.Rat, error) {
	unit, err := MediaUnit(p)
	if err != nil {
		return nil, err
	}
	if p.PerRequest < 0 {
		return nil, fmt.Errorf("pricing: negative per-request price")
	}
	if unit == "" {
		return new(big.Rat).SetInt64(p.PerRequest), nil
	}
	var count int64
	scale := int64(1)
	switch unit {
	case "image":
		count = u.ImageCount
	case "audio_character":
		count = u.AudioCharacters
	case "audio_second":
		count, scale = u.AudioDurationMicros, microsPerSecond
	case "video_second":
		count, scale = u.VideoDurationMicros, microsPerSecond
	}
	if count <= 0 {
		return nil, fmt.Errorf("pricing: missing or nonpositive %s usage", unit)
	}
	if p.Rules["unit_rounding"] == "ceil_second" {
		count = count/scale + boolInt(count%scale != 0)
		scale = 1
	}
	numerator := new(big.Int).Mul(big.NewInt(count), big.NewInt(p.PerRequest))
	amount := new(big.Rat).SetFrac(numerator, big.NewInt(scale))
	if unit == "video_second" {
		_, ppm, err := frozenVideoFacts(p)
		if err != nil {
			return nil, err
		}
		amount.Mul(amount, new(big.Rat).SetFrac64(ppm, 1_000_000))
	}
	return amount, nil
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// WithMediaUnits translates decimal provider units into typed integer usage.
// Seconds must be exactly representable in microseconds; image/character
// counts must be integers. Existing authoritative typed usage takes precedence.
// Callers select the action-specific unit, never infer it from an amount.
func WithMediaUnits(u gateway.Usage, unit, decimalUnits string) (gateway.Usage, error) {
	if err := validateUsage(u); err != nil {
		return u, err
	}
	var dimension *int64
	scale := int64(1)
	switch unit {
	case "image":
		dimension = &u.ImageCount
	case "audio_character":
		dimension = &u.AudioCharacters
	case "audio_second":
		dimension, scale = &u.AudioDurationMicros, microsPerSecond
	case "video_second":
		dimension, scale = &u.VideoDurationMicros, microsPerSecond
	default:
		return u, fmt.Errorf("pricing: unknown media unit %q", unit)
	}
	if *dimension > 0 {
		return u, nil
	}
	units, err := exactNumber(decimalUnits)
	if err != nil || units.Sign() <= 0 {
		return u, fmt.Errorf("pricing: invalid %s units", unit)
	}
	units.Mul(units, new(big.Rat).SetInt64(scale))
	if !units.IsInt() {
		return u, fmt.Errorf("pricing: %s units exceed dimension precision", unit)
	}
	if !units.Num().IsInt64() {
		return u, credits.ErrOverflow
	}
	*dimension = units.Num().Int64()
	return u, nil
}
