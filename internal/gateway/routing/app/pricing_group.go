package app

import (
	"sort"
	"strings"

	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
)

const pricingVersion = "9d31d94c6e1ec64de1cf66f93783919fa08f15504215145e869f095730c9f728"

type PricingPayload struct {
	Data               []gatewaydomain.Pricing                 `json:"data"`
	PricedModels       []string                                `json:"priced_models"`
	PricedModelDetails []gatewaydomain.Pricing                 `json:"priced_model_details"`
	Vendors            []gatewaydomain.PricingVendor           `json:"vendors"`
	GroupRatio         map[string]float64                      `json:"group_ratio"`
	UsableGroup        map[string]string                       `json:"usable_group"`
	SupportedEndpoint  map[string]gatewaycontract.EndpointInfo `json:"supported_endpoint"`
	AutoGroups         []string                                `json:"auto_groups"`
	PricingVersion     string                                  `json:"pricing_version"`
}

// loadPricedModelDetails returns complete site-level billing details without
// exposing the internal groups that made a model visible to the projection.
func loadPricedModelDetails(pricing []gatewaydomain.Pricing) []gatewaydomain.Pricing {
	byName := make(map[string]gatewaydomain.Pricing, len(pricing))
	for _, item := range pricing {
		key := strings.ToLower(strings.TrimSpace(item.ModelName))
		if key == "" {
			continue
		}
		item.EnableGroup = []string{}
		item.PricingVersion = pricingVersion
		byName[key] = item
	}
	for _, modelName := range gatewaystore.GetConfiguredModelBillingNames() {
		key := strings.ToLower(strings.TrimSpace(modelName))
		if key == "" {
			continue
		}
		if _, ok := byName[key]; ok {
			continue
		}
		if detail, ok := configuredModelBillingDetail(modelName); ok {
			byName[key] = detail
		}
	}

	result := make([]gatewaydomain.Pricing, 0, len(byName))
	for _, detail := range byName {
		result = append(result, detail)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].ModelName) < strings.ToLower(result[j].ModelName)
	})
	return result
}

func configuredModelBillingDetail(modelName string) (gatewaydomain.Pricing, bool) {
	detail := gatewaydomain.Pricing{
		ModelName:      modelName,
		EnableGroup:    []string{},
		PricingVersion: pricingVersion,
	}
	if price, ok := gatewaystore.GetModelPrice(modelName, false); ok {
		detail.QuotaType = 1
		detail.ModelPrice = price
	} else if ratio, ok, _ := gatewaystore.GetModelRatio(modelName); ok {
		detail.QuotaType = 0
		detail.ModelRatio = ratio
		detail.CompletionRatio = gatewaystore.GetCompletionRatio(modelName)
	} else if gatewaystore.GetBillingMode(modelName) != gatewaystore.BillingModeTieredExpr {
		return gatewaydomain.Pricing{}, false
	}
	if cacheRatio, ok := gatewaystore.GetCacheRatio(modelName); ok {
		detail.CacheRatio = &cacheRatio
	}
	if createCacheRatio, ok := gatewaystore.GetCreateCacheRatio(modelName); ok {
		detail.CreateCacheRatio = &createCacheRatio
	}
	if imageRatio, ok := gatewaystore.GetImageRatio(modelName); ok {
		detail.ImageRatio = &imageRatio
	}
	if gatewaystore.ContainsAudioRatio(modelName) {
		audioRatio := gatewaystore.GetAudioRatio(modelName)
		detail.AudioRatio = &audioRatio
	}
	if gatewaystore.ContainsAudioCompletionRatio(modelName) {
		audioCompletionRatio := gatewaystore.GetAudioCompletionRatio(modelName)
		detail.AudioCompletionRatio = &audioCompletionRatio
	}
	if gatewaystore.GetBillingMode(modelName) == gatewaystore.BillingModeTieredExpr {
		if expr, ok := gatewaystore.GetBillingExpr(modelName); ok && strings.TrimSpace(expr) != "" {
			detail.BillingMode = gatewaystore.BillingModeTieredExpr
			detail.BillingExpr = expr
		}
	}
	return detail, true
}

// loadPricedModelNames returns every model that has a site-level price or is
// already present in the pricing projection. Marketplace-only models can be
// absent from the projection because they are not backed by an official
// channel, but their global model price still makes them priced models for
// channel configuration purposes.
func loadPricedModelNames(pricing []gatewaydomain.Pricing) []string {
	seen := make(map[string]struct{}, len(pricing))
	result := make([]string, 0, len(pricing))
	for _, item := range pricing {
		if item.ModelName == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(item.ModelName))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item.ModelName)
	}
	for _, modelName := range gatewaystore.GetConfiguredModelBillingNames() {
		if modelName == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(modelName))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, modelName)
	}
	sort.Strings(result)
	return result
}

func loadGatewayPricing() []gatewaydomain.Pricing {
	return gatewaystore.LoadPricing()
}

func loadGatewayVendors() []gatewaydomain.PricingVendor {
	return gatewaystore.LoadPricingVendors()
}

func loadGatewaySupportedEndpointMap() map[string]gatewaycontract.EndpointInfo {
	return gatewaystore.LoadSupportedEndpointMap()
}

func resolveUserGroup(userID int, hasUser bool) string {
	if !hasUser || userID <= 0 {
		return ""
	}
	user, err := identitystore.LoadUserCacheSnapshot(userID)
	if err != nil {
		return ""
	}
	return user.Group
}

func resolveUsableGroups(userID int, hasUser bool) map[string]string {
	return GetUserUsableGroups(resolveUserGroup(userID, hasUser))
}

func visibleGroupRatios(userGroup string, usableGroup map[string]string) map[string]float64 {
	groupRatio := make(map[string]float64)
	for name, ratio := range gatewaystore.GetGroupRatioCopy() {
		groupRatio[name] = ratio
	}
	if userGroup != "" {
		for groupName := range groupRatio {
			if ratio, ok := gatewaystore.GetGroupGroupRatio(userGroup, groupName); ok {
				groupRatio[groupName] = ratio
			}
		}
	}
	for groupName := range gatewaystore.GetGroupRatioCopy() {
		if _, ok := usableGroup[groupName]; !ok {
			delete(groupRatio, groupName)
		}
	}
	return groupRatio
}

func filterPricingByUsableGroups(pricing []gatewaydomain.Pricing, usableGroup map[string]string) []gatewaydomain.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []gatewaydomain.Pricing{}
	}

	filtered := make([]gatewaydomain.Pricing, 0, len(pricing))
	for _, item := range pricing {
		for _, group := range item.EnableGroup {
			if group == "all" {
				filtered = append(filtered, item)
				goto nextPricingItem
			}
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	nextPricingItem:
	}
	return filtered
}

func BuildPricingPayload(userID int, hasUser bool) PricingPayload {
	allPricing := loadGatewayPricing()
	pricing := filterPricingByUsableGroups(allPricing, resolveUsableGroups(userID, hasUser))
	userGroup := resolveUserGroup(userID, hasUser)
	usableGroup := GetUserUsableGroups(userGroup)
	groupRatio := visibleGroupRatios(userGroup, usableGroup)

	return PricingPayload{
		Data:               pricing,
		PricedModels:       loadPricedModelNames(allPricing),
		PricedModelDetails: loadPricedModelDetails(allPricing),
		Vendors:            loadGatewayVendors(),
		GroupRatio:         groupRatio,
		UsableGroup:        usableGroup,
		SupportedEndpoint:  loadGatewaySupportedEndpointMap(),
		AutoGroups:         GetUserAutoGroup(userGroup),
		PricingVersion:     pricingVersion,
	}
}

func BuildAllGroupNames() []string {
	groupNames := make([]string, 0)
	for groupName := range gatewaystore.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	return groupNames
}

func BuildUserGroupsPayload(userID int) map[string]map[string]any {
	userGroup, _ := identitystore.LoadUserGroup(userID, false)
	userUsableGroups := GetUserUsableGroups(userGroup)
	usableGroups := make(map[string]map[string]any)

	for groupName := range gatewaystore.GetGroupRatioCopy() {
		if desc, ok := userUsableGroups[groupName]; ok {
			subscriptionPolicy := gatewaystore.GetSubscriptionGroupPolicy(groupName)
			usableGroups[groupName] = map[string]any{
				"ratio":                GetUserGroupRatio(userGroup, groupName),
				"desc":                 desc,
				"subscription_enabled": subscriptionPolicy.Enabled,
				"subscription_ratio":   subscriptionPolicy.Multiplier,
			}
		}
	}

	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]any{
			"ratio": "自动",
			"desc":  gatewaygroups.GetUsableGroupDescription("auto"),
		}
	}
	if zeroHour, err := commerceapp.BuildZeroHourOverview(userID); err == nil && zeroHour.Active {
		usableGroups[commerceapp.ZeroHourGroup] = map[string]any{
			"ratio": 0,
			"desc":  "盲盒 0 倍率卡生效中，仅限 " + commerceapp.MultiplierCardRouteGroup() + " 的非生图模型",
		}
	}
	return usableGroups
}

func ResetModelRatio() error {
	defaultStr := gatewaystore.DefaultModelRatio2JSONString()
	if err := platformstore.UpdateOption("ModelRatio", defaultStr); err != nil {
		return err
	}
	return gatewaystore.UpdateModelRatioByJSONString(defaultStr)
}

func ExposedRatioConfig() (any, bool) {
	if !gatewaystore.IsExposeRatioEnabled() {
		return nil, false
	}
	return gatewaystore.GetExposedData(), true
}
