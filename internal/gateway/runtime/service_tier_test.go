package runtime

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestCodexSpeedTierPreservedAndPriced(t *testing.T) {
	for tier, multiplier := range map[string]int{"priority": 2, "ultrafast": 6, "auto": 1, "default": 1, "flex": 1, "": 1} {
		t.Run(tier, func(t *testing.T) {
			info := &RelayInfo{ServiceTier: tier}
			require.Equal(t, 120*multiplier, info.ServiceTierQuota(120))
			require.Zero(t, info.ServiceTierQuota(0))
			if multiplier > 1 {
				require.Equal(t, math.MaxInt, info.ServiceTierQuota(math.MaxInt))
				for _, key := range []string{`service_tier`, `\u0073ervice_tier`} {
					filtered, err := RemoveDisabledFields([]byte(`{"`+key+`":"`+tier+`","store":true}`), dto.ChannelOtherSettings{DisableStore: true}, false)
					require.NoError(t, err)
					var body map[string]any
					require.NoError(t, json.Unmarshal(filtered, &body))
					require.Equal(t, tier, body["service_tier"])
					require.NotContains(t, body, "store")
				}
			}
			require.Equal(t, tier, requestServiceTier(&dto.OpenAIResponsesRequest{ServiceTier: tier}))
			require.Equal(t, tier, requestServiceTier(&dto.OpenAIResponsesCompactionRequest{ServiceTier: tier}))
			encoded, err := json.Marshal(tier)
			require.NoError(t, err)
			require.Equal(t, tier, requestServiceTier(&dto.GeneralOpenAIRequest{ServiceTier: encoded}))
		})
	}
}
