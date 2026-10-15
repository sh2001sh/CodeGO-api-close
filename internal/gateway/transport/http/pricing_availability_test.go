package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/stretchr/testify/require"
)

func TestPricingSeparatesBillingConfigurationFromAvailableModels(t *testing.T) {
	withTieredBillingConfig(t, map[string]string{"zz-config-only": "tiered_expr"},
		map[string]string{"zz-config-only": `tier("base", p * 1 + c * 2)`})
	db := setupModelListControllerTestDB(t)
	gatewaystore.InvalidatePricingCache()
	t.Cleanup(gatewaystore.InvalidatePricingCache)
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 1, Status: 1}).Error)
	require.NoError(t, db.Create(&gatewayschema.Ability{Group: "default", Model: "gpt-4", ChannelId: 1, Enabled: true}).Error)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	GetPricing(ctx)
	var payload struct {
		Success   bool     `json:"success"`
		Available []string `json:"available_models"`
		Priced    []string `json:"priced_models"`
	}
	require.NoError(t, platformencoding.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Contains(t, payload.Available, "gpt-4")
	require.NotContains(t, payload.Available, "zz-config-only")
	require.Contains(t, payload.Priced, "zz-config-only", "channel configuration must still have access to all billing definitions")
}
