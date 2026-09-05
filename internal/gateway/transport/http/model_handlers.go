package http

import (
	"fmt"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
)

func ListModels(c *gin.Context, modelType int) {
	userID := c.GetInt("id")
	tokenModelLimitEnabled := httpctx.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	tokenModelLimit := map[string]bool{}
	if tokenModelLimitEnabled {
		if value, ok := httpctx.GetContextKey(c, constant.ContextKeyTokenModelLimit); ok {
			tokenModelLimit = value.(map[string]bool)
		}
	}
	tokenGroup := httpctx.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if marketplaceapp.IsMarketplaceTokenGroup(tokenGroup) && !marketplaceapp.IsMarketplaceAutoTokenGroup(tokenGroup) {
		// TokenAuth resolves market:<id> to the actual internal routing group.
		// Model-list routes do not run the distributor that normally performs
		// this replacement for relay requests.
		if resolvedGroup := httpctx.GetContextKeyString(c, constant.ContextKeyUsingGroup); resolvedGroup != "" {
			tokenGroup = resolvedGroup
		}
	}

	var userOpenAIModels []dto.OpenAIModels
	var err error
	normalizedTokenGroup := gatewayroutingapp.NormalizeTokenGroup(tokenGroup)
	marketplaceAutoToken := marketplaceapp.IsMarketplaceAutoTokenGroup(tokenGroup)
	marketplaceRoutePool := marketplaceapp.IsMarketplaceRoutePoolTokenGroup(tokenGroup)
	if !tokenModelLimitEnabled && marketplaceRoutePool {
		poolModels, poolErr := marketplaceapp.ListRoutePoolModels(userID, marketplaceapp.RoutePoolIDFromTokenGroup(tokenGroup))
		if poolErr != nil {
			err = poolErr
		} else {
			userOpenAIModels = gatewayroutingapp.CollectOpenAIModelsForNames(userID, poolModels)
		}
	} else if !tokenModelLimitEnabled && (normalizedTokenGroup == gatewayroutingapp.AutoGroupName || marketplaceAutoToken) && marketplaceapp.HasConfiguredAutoRoutePool(userID) {
		var poolModels []string
		poolModels, _, err = marketplaceapp.ListSelectedAutoRouteModels(userID)
		if err == nil {
			userOpenAIModels = gatewayroutingapp.CollectOpenAIModelsForNames(userID, poolModels)
		}
	} else {
		userOpenAIModels, err = gatewayroutingapp.CollectUserOpenAIModels(userID, tokenModelLimitEnabled, tokenModelLimit, normalizedTokenGroup)
	}
	if err != nil {
		c.JSON(stdhttp.StatusOK, gin.H{
			"success": false,
			"message": "get user group failed",
		})
		return
	}
	if httpctx.GetContextKeyBool(c, constant.ContextKeyZeroHourActive) {
		filtered := userOpenAIModels[:0]
		for _, model := range userOpenAIModels {
			if !gatewaycontract.IsImageGenerationModel(model.Id) {
				filtered = append(filtered, model)
			}
		}
		userOpenAIModels = filtered
	}

	switch modelType {
	case constant.ChannelTypeAnthropic:
		anthropicModels := gatewayroutingapp.BuildAnthropicModels(userOpenAIModels)
		firstID := ""
		lastID := ""
		if len(anthropicModels) > 0 {
			firstID = anthropicModels[0].ID
			lastID = anthropicModels[len(anthropicModels)-1].ID
		}
		c.JSON(stdhttp.StatusOK, gin.H{
			"data":     anthropicModels,
			"first_id": firstID,
			"has_more": false,
			"last_id":  lastID,
		})
	case constant.ChannelTypeGemini:
		c.JSON(stdhttp.StatusOK, gin.H{
			"models":        gatewayroutingapp.BuildGeminiModels(userOpenAIModels),
			"nextPageToken": nil,
		})
	default:
		c.JSON(stdhttp.StatusOK, gin.H{
			"success": true,
			"data":    userOpenAIModels,
			"object":  "list",
		})
	}
}

func ChannelListModels(c *gin.Context) {
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"data":    gatewayroutingapp.AllChannelModels(),
	})
}

func DashboardListModels(c *gin.Context) {
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"data":    gatewayroutingapp.DashboardModels(),
	})
}

func EnabledListModels(c *gin.Context) {
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"data":    gatewayroutingapp.EnabledModels(),
	})
}

func RetrieveModel(c *gin.Context, modelType int) {
	modelID := c.Param("model")
	if aiModel, ok := gatewayroutingapp.FindOpenAIModel(modelID); ok {
		switch modelType {
		case constant.ChannelTypeAnthropic:
			c.JSON(stdhttp.StatusOK, gatewayroutingapp.BuildAnthropicModels([]dto.OpenAIModels{aiModel})[0])
		default:
			c.JSON(stdhttp.StatusOK, aiModel)
		}
		return
	}

	openAIError := types.OpenAIError{
		Message: fmt.Sprintf("The model '%s' does not exist", modelID),
		Type:    "invalid_request_error",
		Param:   "model",
		Code:    "model_not_found",
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"error": openAIError,
	})
}
