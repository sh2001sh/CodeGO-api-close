package http

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

type tokenConnectivityTestRequest struct {
	Model string `json:"model"`
}

func TestTokenConnectivity(c *gin.Context) {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	var req tokenConnectivityTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		httpapi.ApiError(c, fmt.Errorf("请选择需要测试的模型"))
		return
	}
	token, err := identityapp.GetUserToken(c.GetInt("id"), tokenID)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	models, err := tokenTestModels(token)
	if err != nil || !containsTokenTestModel(models, modelName) {
		if err != nil {
			httpapi.ApiError(c, err)
		} else {
			httpapi.ApiError(c, fmt.Errorf("该 API Key 分组不支持模型 %s", modelName))
		}
		return
	}
	channelID, binding, err := resolveTokenTestChannel(c, token, modelName)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	seconds, report, apiErr, testErr := gatewayexecutionapp.TestMarketplaceChannelByID(
		channelID, modelName, "", false, tokenConnectivityBillingOptions(token, binding),
	)
	if testErr != nil {
		httpapi.ApiError(c, testErr)
		return
	}
	if apiErr != nil {
		httpapi.ApiError(c, apiErr)
		return
	}
	httpapi.ApiSuccess(c, gin.H{
		"model": modelName, "group": binding.InternalGroup, "channel_id": channelID,
		"latency_ms": int64(seconds * 1000), "quota_charged": report.QuotaCharged,
		"billing_source": report.BillingSource, "request_id": report.RequestID,
	})
}

func tokenConnectivityBillingOptions(token *identityschema.Token, binding *marketplaceapp.RoutingBinding) gatewayexecutionapp.MarketplaceChannelTestOptions {
	options := gatewayexecutionapp.MarketplaceChannelTestOptions{
		UserID: token.UserId, InternalGroup: binding.InternalGroup,
	}
	if binding.SourceType == marketplacedomain.SourceTypeMarketplaceUser {
		options.MarketplaceGroupID = binding.GroupID
		options.MarketplaceOwnerID = binding.OwnerUserID
		options.CreditPoolPolicy = binding.CreditPoolPolicy
		options.Multiplier = binding.Multiplier
		options.ModelPrices = binding.ModelPrices
	}
	return options
}

func tokenTestModels(token *identityschema.Token) ([]string, error) {
	group := gatewayroutingapp.NormalizeTokenGroup(token.Group)
	if marketplaceapp.IsMarketplaceRoutePoolTokenGroup(group) {
		return marketplaceapp.ListRoutePoolModels(token.UserId, marketplaceapp.RoutePoolIDFromTokenGroup(group))
	}
	if marketplaceapp.IsMarketplaceTokenGroup(group) && !marketplaceapp.IsMarketplaceAutoTokenGroup(group) {
		binding, err := marketplaceapp.ResolveTokenGroupBinding(group, token.UserId)
		if err != nil {
			return nil, err
		}
		return binding.Models, nil
	}
	if group == gatewayroutingapp.AutoGroupName || marketplaceapp.IsMarketplaceAutoTokenGroup(group) {
		if marketplaceapp.HasConfiguredAutoRoutePool(token.UserId) || marketplaceapp.IsMarketplaceAutoTokenGroup(group) {
			pool, err := marketplaceapp.ListAutoRoutePool(token.UserId)
			if err != nil {
				return nil, err
			}
			if err == nil {
				models := make(map[string]struct{})
				for _, item := range pool.Items {
					if item.Selected && marketplaceapp.MultiplierWithinLimit(item.Multiplier, token.MarketplaceMultiplierLimit) {
						for _, model := range item.Models {
							models[model] = struct{}{}
						}
					}
				}
				result := make([]string, 0, len(models))
				for model := range models {
					result = append(result, model)
				}
				return result, nil
			}
		}
	}
	return identityapp.ListUserModelsForGroup(token.UserId, group)
}

func resolveTokenTestChannel(c *gin.Context, token *identityschema.Token, modelName string) (int, *marketplaceapp.RoutingBinding, error) {
	tokenGroup := gatewayroutingapp.NormalizeTokenGroup(token.Group)
	if marketplaceapp.IsMarketplaceRoutePoolTokenGroup(tokenGroup) {
		bindings, _, err := marketplaceapp.ResolveRoutePoolBindings(token.UserId, marketplaceapp.RoutePoolIDFromTokenGroup(tokenGroup), modelName, token.MarketplaceMultiplierLimit)
		if err != nil {
			return 0, nil, err
		}
		for _, binding := range bindings {
			if channelID, groupName, selectErr := selectTokenTestChannel(c, binding.InternalGroup, modelName); selectErr == nil {
				binding.InternalGroup = groupName
				return channelID, &binding, nil
			}
		}
		return 0, nil, fmt.Errorf("路由池当前没有可用于测试的渠道")
	}
	if marketplaceapp.IsMarketplaceTokenGroup(tokenGroup) && !marketplaceapp.IsMarketplaceAutoTokenGroup(tokenGroup) {
		binding, err := marketplaceapp.ResolveTokenGroupBinding(tokenGroup, token.UserId)
		if err != nil {
			return 0, nil, err
		}
		channelID, groupName, err := selectTokenTestChannel(c, binding.InternalGroup, modelName)
		if err != nil {
			return 0, nil, err
		}
		binding.InternalGroup = groupName
		return channelID, binding, nil
	}
	if tokenGroup == gatewayroutingapp.AutoGroupName || marketplaceapp.IsMarketplaceAutoTokenGroup(tokenGroup) {
		if marketplaceapp.HasConfiguredAutoRoutePool(token.UserId) || marketplaceapp.IsMarketplaceAutoTokenGroup(tokenGroup) {
			bindings, err := marketplaceapp.ResolveAutoRouteBindings(token.UserId, modelName, token.MarketplaceMultiplierLimit)
			if err != nil {
				return 0, nil, err
			}
			for _, binding := range bindings {
				if channelID, groupName, selectErr := selectTokenTestChannel(c, binding.InternalGroup, modelName); selectErr == nil {
					binding.InternalGroup = groupName
					return channelID, &binding, nil
				}
			}
			return 0, nil, fmt.Errorf("Auto 路由池当前没有可用于测试的渠道")
		}
	}
	channelID, groupName, err := selectTokenTestChannel(c, tokenGroup, modelName)
	if err != nil {
		return 0, nil, err
	}
	return channelID, &marketplaceapp.RoutingBinding{
		InternalGroup: groupName, SourceType: marketplacedomain.SourceTypeOfficial,
		CreditPoolPolicy: marketplacedomain.CreditPolicyOfficialDefault, Multiplier: 1,
	}, nil
}

func selectTokenTestChannel(c *gin.Context, groupName, modelName string) (int, string, error) {
	retry := 0
	channel, selectedGroup, err := gatewayroutingapp.CacheGetRandomSatisfiedChannel(&gatewayroutingapp.RetryParam{Ctx: c, TokenGroup: groupName, ModelName: modelName, Retry: &retry})
	if err != nil || channel == nil {
		if err != nil {
			return 0, "", err
		}
		return 0, "", fmt.Errorf("分组 %s 没有可用于测试的渠道", groupName)
	}
	return channel.Id, selectedGroup, nil
}

func containsTokenTestModel(models []string, modelName string) bool {
	for _, model := range models {
		if strings.EqualFold(strings.TrimSpace(model), modelName) {
			return true
		}
	}
	return false
}
