package app

import (
	"encoding/json"
	"sort"
	"strings"

	auditprojection "github.com/sh2001sh/new-api/internal/audit/projection"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

func BuildModelRankingSummary(hours int) (auditprojection.SummaryAllResult, error) {
	scope, err := publicModelRankingScope()
	if err != nil {
		return auditprojection.SummaryAllResult{}, err
	}
	return auditprojection.QueryRankingSummary(hours, scope)
}

func BuildModelRankingTrend(model string, hours int) (auditprojection.QueryResult, error) {
	scope, err := publicModelRankingScope()
	if err != nil {
		return auditprojection.QueryResult{}, err
	}
	return auditprojection.QueryRankingTrend(model, hours, scope)
}

// Rankings are public even for a signed-in owner: private invitations and
// account-specific usable groups never widen their analytics scope.
func publicModelRankingScope() (map[string][]string, error) {
	var groups []marketplaceschema.Group
	if err := platformdb.DB.Unscoped().Select("internal_group_name, channel_id, visibility, published_at, verification_status, lifecycle_status, moderation_status").Find(&groups).Error; err != nil {
		return nil, err
	}
	marketGroups := make(map[string]struct{}, len(groups))
	publicMarketGroups := make(map[string][]string)
	for _, group := range groups {
		marketGroups[group.InternalGroupName] = struct{}{}
		published := group.PublishedAt != nil || (group.VerificationStatus == marketplacedomain.VerificationPassed && group.LifecycleStatus != marketplacedomain.LifecycleDraft)
		if group.Visibility == marketplacedomain.VisibilityPublic && published && group.ModerationStatus != "failed" {
			publicMarketGroups[group.ChannelID] = append(publicMarketGroups[group.ChannelID], group.InternalGroupName)
		}
	}
	var metadata []gatewayschema.Model
	if err := platformdb.DB.Select("model_name, status, name_rule").Order("id ASC").Find(&metadata).Error; err != nil {
		return nil, err
	}
	pairs := make(map[string]map[string]struct{})
	add := func(group, model string) {
		group, model = strings.TrimSpace(group), strings.TrimSpace(model)
		if group == "" || group == "auto" || group == "all" || model == "" || !modelRankingVisible(model, metadata) {
			return
		}
		if pairs[group] == nil {
			pairs[group] = make(map[string]struct{})
		}
		pairs[group][model] = struct{}{}
	}
	// Status affects routing availability, not historical usage. Read the small
	// channel identity projection without credentials or an active-only filter.
	var official []gatewayschema.Channel
	if err := platformdb.DB.Select("models, "+rankingGroupColumn()+", channel_scope").Where("channel_scope <> ? OR channel_scope IS NULL", gatewayschema.ChannelScopeExternal).Find(&official).Error; err != nil {
		return nil, err
	}
	publicOfficialGroups := GetUserUsableGroups("")
	for _, channel := range official {
		for _, group := range strings.Split(channel.Group, ",") {
			group = strings.TrimSpace(group)
			if _, market := marketGroups[group]; market {
				continue
			}
			if _, public := publicOfficialGroups[group]; !public {
				continue
			}
			for _, model := range strings.Split(channel.Models, ",") {
				add(group, model)
			}
		}
	}
	if len(publicMarketGroups) > 0 {
		ids := make([]string, 0, len(publicMarketGroups))
		for id := range publicMarketGroups {
			ids = append(ids, id)
		}
		var channels []marketplaceschema.Channel
		if err := platformdb.DB.Unscoped().Select("id, declared_models").Where("id IN ?", ids).Find(&channels).Error; err != nil {
			return nil, err
		}
		for _, channel := range channels {
			var models []string
			if err := json.Unmarshal([]byte(channel.DeclaredModels), &models); err != nil {
				return nil, err
			}
			for _, group := range publicMarketGroups[channel.ID] {
				for _, model := range models {
					add(group, model)
				}
			}
		}
	}
	scope := make(map[string][]string, len(pairs))
	for group, models := range pairs {
		for model := range models {
			scope[group] = append(scope[group], model)
		}
		sort.Strings(scope[group])
	}
	return scope, nil
}

func modelRankingVisible(name string, metadata []gatewayschema.Model) bool {
	// Match the catalog's exact > prefix > suffix > contains precedence.
	for _, rule := range []int{gatewayschema.NameRuleExact, gatewayschema.NameRulePrefix, gatewayschema.NameRuleSuffix, gatewayschema.NameRuleContains} {
		for _, model := range metadata {
			if model.NameRule != rule {
				continue
			}
			matches := name == model.ModelName
			switch rule {
			case gatewayschema.NameRulePrefix:
				matches = strings.HasPrefix(name, model.ModelName)
			case gatewayschema.NameRuleSuffix:
				matches = strings.HasSuffix(name, model.ModelName)
			case gatewayschema.NameRuleContains:
				matches = strings.Contains(name, model.ModelName)
			}
			if matches {
				return model.Status == 1
			}
		}
	}
	return true
}

func rankingGroupColumn() string {
	if platformdb.UsingMySQL {
		return "`group`"
	}
	return `"group"`
}
