package http

import (
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

type responsesBackgroundRoutingContext struct {
	UsingGroup                string                                      `json:"using_group"`
	TokenGroup                string                                      `json:"token_group"`
	MarketplaceGroupID        string                                      `json:"marketplace_group_id,omitempty"`
	MarketplaceOwnerID        int                                         `json:"marketplace_owner_id,omitempty"`
	MarketplaceSourceType     string                                      `json:"marketplace_source_type,omitempty"`
	MarketplaceCreditPolicy   string                                      `json:"marketplace_credit_policy,omitempty"`
	MarketplaceMultiplier     float64                                     `json:"marketplace_multiplier,omitempty"`
	MarketplaceModelPrices    map[string]marketplaceapp.ChannelModelPrice `json:"marketplace_model_prices,omitempty"`
	RoutePoolID               int64                                       `json:"route_pool_id,omitempty"`
	ProcurementCostMultiplier float64                                     `json:"procurement_cost_multiplier,omitempty"`
	FaultDomain               string                                      `json:"fault_domain,omitempty"`
}

func captureResponsesBackgroundRoutingContext(c *gin.Context) (string, error) {
	snapshot := responsesBackgroundRoutingContext{
		UsingGroup:              httpctx.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		TokenGroup:              httpctx.GetContextKeyString(c, constant.ContextKeyTokenGroup),
		MarketplaceGroupID:      httpctx.GetContextKeyString(c, constant.ContextKeyMarketplaceGroupID),
		MarketplaceOwnerID:      httpctx.GetContextKeyInt(c, constant.ContextKeyMarketplaceOwnerID),
		MarketplaceSourceType:   httpctx.GetContextKeyString(c, constant.ContextKeyMarketplaceSourceType),
		MarketplaceCreditPolicy: httpctx.GetContextKeyString(c, constant.ContextKeyMarketplaceCreditPolicy),
		MarketplaceMultiplier:   httpctx.GetContextKeyFloat64(c, constant.ContextKeyMarketplaceMultiplier),
		FaultDomain:             c.GetString("channel_fault_domain"),
	}
	if prices, found := httpctx.GetContextKeyType[map[string]marketplaceapp.ChannelModelPrice](c, constant.ContextKeyMarketplaceModelPrices); found {
		snapshot.MarketplaceModelPrices = prices
	}
	if selection, found := gatewayroutingapp.GetRoutePoolSelection(c); found {
		snapshot.RoutePoolID = selection.PoolID
		snapshot.ProcurementCostMultiplier = selection.ProcurementCostMultiplier
	}
	raw, err := platformencoding.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return platformsecurity.EncryptSecret(string(raw))
}

func restoreResponsesBackgroundRoutingContext(c *gin.Context, ciphertext string) error {
	raw, err := platformsecurity.DecryptSecret(ciphertext)
	if err != nil {
		return err
	}
	var snapshot responsesBackgroundRoutingContext
	if err := platformencoding.Unmarshal([]byte(raw), &snapshot); err != nil {
		return err
	}
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, snapshot.UsingGroup)
	httpctx.SetContextKey(c, constant.ContextKeyTokenGroup, snapshot.TokenGroup)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceGroupID, snapshot.MarketplaceGroupID)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceOwnerID, snapshot.MarketplaceOwnerID)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceSourceType, snapshot.MarketplaceSourceType)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceCreditPolicy, snapshot.MarketplaceCreditPolicy)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceMultiplier, snapshot.MarketplaceMultiplier)
	httpctx.SetContextKey(c, constant.ContextKeyMarketplaceModelPrices, snapshot.MarketplaceModelPrices)
	if snapshot.RoutePoolID > 0 && snapshot.ProcurementCostMultiplier > 0 {
		gatewayroutingapp.SetRoutePoolSelectionSnapshot(c, gatewayroutingapp.RoutePoolSelection{
			PoolID: snapshot.RoutePoolID, ProcurementCostMultiplier: snapshot.ProcurementCostMultiplier,
		}, snapshot.FaultDomain)
	}
	if snapshot.FaultDomain != "" {
		c.Set("channel_fault_domain", snapshot.FaultDomain)
	}
	return nil
}
