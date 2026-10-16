package runtime

import (
	"encoding/json"
	"math"

	"github.com/sh2001sh/new-api/dto"
)

// Codex sends Fast as priority and UltraFast as ultrafast. Keep the multiplier
// separate from group pricing so retries and subscription conversion cannot
// accidentally apply it twice.
func ServiceTierMultiplier(tier string) int {
	switch tier {
	case "priority":
		return 2
	case "ultrafast":
		return 6
	default:
		return 1
	}
}

func requestServiceTier(request dto.Request) string {
	switch req := request.(type) {
	case *dto.OpenAIResponsesRequest:
		return req.ServiceTier
	case *dto.OpenAIResponsesCompactionRequest:
		return req.ServiceTier
	case *dto.GeneralOpenAIRequest:
		var tier string
		_ = json.Unmarshal(req.ServiceTier, &tier)
		return tier
	default:
		return ""
	}
}

func (info *RelayInfo) ServiceTierQuota(quota int) int {
	if info == nil || quota <= 0 {
		return quota
	}
	multiplier := ServiceTierMultiplier(info.ServiceTier)
	if quota > math.MaxInt/multiplier {
		return math.MaxInt
	}
	return quota * multiplier
}
