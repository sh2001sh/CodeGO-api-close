package channelmarket

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// GroupQuality reports a timestamped window of request outcomes and billing.
// It is not an uptime promise. Missing measurements stay absent; the cache
// schema cannot distinguish no token samples from a zero hit rate, so zero
// cache values are deliberately not advertised as measurements.
type GroupQuality struct {
	WindowHours          int       `json:"window_hours"`
	Rank                 int       `json:"rank"`
	Score                float64   `json:"score"`
	SuccessRate          *float64  `json:"success_rate,omitempty"`
	WilsonSuccessRate    *float64  `json:"wilson_success_rate,omitempty"`
	CacheHitRate         *float64  `json:"cache_hit_rate,omitempty"`
	RequestCount         int64     `json:"request_count"`
	AverageChargeCredits *string   `json:"average_charge_credits,omitempty"`
	Observing            bool      `json:"observing"`
	CalculatedAt         time.Time `json:"calculated_at"`
}

func parseGroupQuality(raw []byte) (*GroupQuality, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var result struct {
		GroupQuality
		AverageChargeMicro *int64 `json:"average_charge_micro"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.AverageChargeMicro != nil {
		value := publicCreditAmount(*result.AverageChargeMicro, 1_000_000)
		result.AverageChargeCredits = &value
	}
	return &result.GroupQuality, nil
}

func effectiveModelPrices(group ChannelView, baseRaw []byte) (map[string]*PublicModelPrice, error) {
	prices, err := catalog.ParseMarketPrices(group.Prices)
	if err != nil {
		return nil, err
	}
	// An explicit seller map replaces the platform map exactly as it does
	// during settlement. A missing model is unpriced, never a free quote.
	if len(prices) == 0 {
		var baseline []catalog.Price
		decoder := json.NewDecoder(bytes.NewReader(baseRaw))
		decoder.UseNumber()
		if err = decoder.Decode(&baseline); err != nil {
			return nil, err
		}
		for _, price := range baseline {
			prices[price.Model] = price
		}
	}
	result := make(map[string]*PublicModelPrice)
	for _, model := range group.Models {
		if price, found := prices[model]; found {
			result[model] = publicPrice(price, group.MultiplierPPM)
		}
	}
	return result, nil
}
