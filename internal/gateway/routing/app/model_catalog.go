package app

import (
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayproviders "github.com/sh2001sh/new-api/internal/gateway/execution/providers"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformops "github.com/sh2001sh/new-api/internal/platform/opssettings"
)

var openAIModels []dto.OpenAIModels
var openAIModelsMap map[string]dto.OpenAIModels
var channelIDToModels map[int][]string

func init() {
	for i := 0; i < constant.APITypeDummy; i++ {
		if i == constant.APITypeAIProxyLibrary {
			continue
		}
		adaptor := gatewayproviders.NewSyncAdaptor(i)
		if adaptor == nil {
			continue
		}
		channelName := adaptor.GetChannelName()
		modelNames := adaptor.GetModelList()
		for _, modelName := range modelNames {
			openAIModels = append(openAIModels, dto.OpenAIModels{
				Id:      modelName,
				Object:  "model",
				Created: 1626777600,
				OwnedBy: channelName,
			})
		}
	}
	for _, entry := range gatewayproviders.StaticModelCatalogEntries() {
		for _, modelName := range entry.ModelList {
			openAIModels = append(openAIModels, dto.OpenAIModels{
				Id:      modelName,
				Object:  "model",
				Created: 1626777600,
				OwnedBy: entry.ChannelName,
			})
		}
	}
	openAIModelsMap = make(map[string]dto.OpenAIModels)
	for _, aiModel := range openAIModels {
		openAIModelsMap[aiModel.Id] = aiModel
	}
	channelIDToModels = make(map[int][]string)
	for i := 1; i <= constant.ChannelTypeDummy; i++ {
		apiType, success := constant.ChannelTypeToAPIType(i)
		if !success || apiType == constant.APITypeAIProxyLibrary {
			continue
		}
		meta := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: i,
		}}
		adaptor := gatewayproviders.NewSyncAdaptor(apiType)
		if adaptor == nil {
			continue
		}
		adaptor.Init(meta)
		channelIDToModels[i] = adaptor.GetModelList()
	}
	openAIModels = lo.UniqBy(openAIModels, func(m dto.OpenAIModels) string {
		return m.Id
	})
}

func loadGatewayGroupEnabledModels(group string) []string {
	return gatewaystore.LoadGroupEnabledModels(group)
}

func EnabledModelsForGroup(group string) []string {
	return loadGatewayGroupEnabledModels(group)
}

func loadGatewayEnabledModels() []string {
	return gatewaystore.LoadEnabledModels()
}

func loadGatewayModelSupportedEndpointTypes(modelName string) []constant.EndpointType {
	return gatewaystore.LoadModelSupportedEndpointTypes(modelName)
}

func loadGatewayModelEnableGroups(modelName string) []string {
	return gatewaystore.LoadModelEnableGroups(modelName)
}

func loadGatewayModelQuotaTypes(modelName string) []int {
	return gatewaystore.LoadModelQuotaTypes(modelName)
}

// CollectUserOpenAIModels returns the visible models for one user/token context.
func CollectUserOpenAIModels(userID int, tokenModelLimitEnabled bool, tokenModelLimit map[string]bool, tokenGroup string) ([]dto.OpenAIModels, error) {
	if tokenModelLimitEnabled {
		models := make([]string, 0, len(tokenModelLimit))
		for allowModel := range tokenModelLimit {
			models = append(models, allowModel)
		}
		return CollectOpenAIModelsForNames(userID, models), nil
	}

	userGroup, err := identitystore.LoadUserGroup(userID, false)
	if err != nil {
		return nil, err
	}
	tokenGroup = NormalizeTokenGroup(tokenGroup)
	group := tokenGroup

	var models []string
	if tokenGroup == AutoGroupName {
		for _, autoGroup := range GetUserAutoGroup(userGroup) {
			groupModels := loadGatewayGroupEnabledModels(autoGroup)
			for _, groupModel := range groupModels {
				if !platformtext.StringsContains(models, groupModel) {
					models = append(models, groupModel)
				}
			}
		}
	} else {
		models = loadGatewayGroupEnabledModels(group)
	}
	return CollectOpenAIModelsForNames(userID, models), nil
}

// CollectOpenAIModelsForNames builds the public model catalog entries for an
// explicit model set while preserving the user's billing visibility policy.
func CollectOpenAIModelsForNames(userID int, modelNames []string) []dto.OpenAIModels {
	userOpenAIModels := make([]dto.OpenAIModels, 0, len(modelNames))
	acceptUnsetRatioModel := platformops.IsSelfUseModeEnabled()
	if !acceptUnsetRatioModel && userID > 0 {
		userSettings, _ := identitystore.LoadUserSetting(userID, false)
		acceptUnsetRatioModel = userSettings.AcceptUnsetRatioModel
	}

	seen := make(map[string]struct{}, len(modelNames))
	for _, modelName := range modelNames {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		if !acceptUnsetRatioModel && !relaycommon.HasModelBillingConfig(modelName) {
			continue
		}
		if oaiModel, ok := openAIModelsMap[modelName]; ok {
			oaiModel.SupportedEndpointTypes = loadGatewayModelSupportedEndpointTypes(modelName)
			userOpenAIModels = append(userOpenAIModels, oaiModel)
			continue
		}
		userOpenAIModels = append(userOpenAIModels, dto.OpenAIModels{
			Id:                     modelName,
			Object:                 "model",
			Created:                1626777600,
			OwnedBy:                "custom",
			SupportedEndpointTypes: loadGatewayModelSupportedEndpointTypes(modelName),
		})
	}
	return userOpenAIModels
}

// BuildAnthropicModels adapts OpenAI model entries to the Anthropic list shape.
func BuildAnthropicModels(models []dto.OpenAIModels) []dto.AnthropicModel {
	anthropicModels := make([]dto.AnthropicModel, len(models))
	for i, item := range models {
		anthropicModels[i] = dto.AnthropicModel{
			ID:          item.Id,
			CreatedAt:   time.Unix(int64(item.Created), 0).UTC().Format(time.RFC3339),
			DisplayName: item.Id,
			Type:        "model",
		}
	}
	return anthropicModels
}

// BuildGeminiModels adapts OpenAI model entries to the Gemini list shape.
func BuildGeminiModels(models []dto.OpenAIModels) []dto.GeminiModel {
	geminiModels := make([]dto.GeminiModel, len(models))
	for i, item := range models {
		geminiModels[i] = dto.GeminiModel{
			Name:        item.Id,
			DisplayName: item.Id,
		}
	}
	return geminiModels
}

func AllChannelModels() []dto.OpenAIModels {
	return openAIModels
}

func DashboardModels() map[int][]string {
	return channelIDToModels
}

func EnabledModels() []string {
	return loadGatewayEnabledModels()
}

func FindOpenAIModel(modelID string) (dto.OpenAIModels, bool) {
	item, ok := openAIModelsMap[modelID]
	return item, ok
}
