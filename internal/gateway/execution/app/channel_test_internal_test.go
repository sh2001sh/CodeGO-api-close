package app

import (
	"encoding/json"
	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	"github.com/sh2001sh/new-api/internal/billing/domain/billingexpr"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestValidateMarketplaceContentResponseRejectsTokenLimit(t *testing.T) {
	response := &dto.OpenAIResponsesResponse{
		Status:            json.RawMessage(`"incomplete"`),
		IncompleteDetails: &dto.IncompleteDetails{Reasoning: "max_output_tokens"},
	}

	err := validateMarketplaceContentResponse(response)
	require.EqualError(t, err, "模型输出达到长度上限，未生成完整内容")
}

func TestValidateMarketplaceContentResponseAcceptsCompletedResponse(t *testing.T) {
	response := &dto.OpenAIResponsesResponse{Status: json.RawMessage(`"completed"`)}
	require.NoError(t, validateMarketplaceContentResponse(response))
}

func TestAutomaticChannelTestSkipsManuallyDisabledChannel(t *testing.T) {
	require.False(t, shouldAutomaticallyTestChannel(&gatewayschema.Channel{
		Status: constant.ChannelStatusManuallyDisabled,
	}))
	require.True(t, shouldAutomaticallyTestChannel(&gatewayschema.Channel{
		Status: constant.ChannelStatusAutoDisabled,
	}))
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  platformruntime.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}
