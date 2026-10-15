package domain

const (
	ChannelBillingModeToken   = "token"
	ChannelBillingModePerCall = "per_call"
)

type ChannelModelPrice struct {
	BillingMode               string   `json:"billing_mode,omitempty"`
	PricePerCall              float64  `json:"price_per_call,omitempty"`
	InputPricePerMillion      float64  `json:"input_price_per_million,omitempty"`
	OutputPricePerMillion     float64  `json:"output_price_per_million,omitempty"`
	CacheReadPricePerMillion  *float64 `json:"cache_read_price_per_million,omitempty"`
	CacheWritePricePerMillion *float64 `json:"cache_write_price_per_million,omitempty"`
}

// EffectiveBillingMode keeps legacy JSON records compatible by treating an
// omitted billing mode as token-based pricing.
func (price ChannelModelPrice) EffectiveBillingMode() string {
	if price.BillingMode == ChannelBillingModePerCall {
		return ChannelBillingModePerCall
	}
	return ChannelBillingModeToken
}
