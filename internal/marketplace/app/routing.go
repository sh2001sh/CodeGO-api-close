package app

import (
	"errors"
	"strings"

	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

func TokenGroupValue(groupID string) string {
	return marketplacedomain.TokenGroupPrefix + strings.TrimSpace(groupID)
}

func RoutePoolTokenGroupValue(poolID string) string {
	return marketplacedomain.TokenGroupPrefix + "pool:" + strings.TrimSpace(poolID)
}

func IsMarketplaceTokenGroup(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), marketplacedomain.TokenGroupPrefix)
}

func IsMarketplaceAutoTokenGroup(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), marketplacedomain.TokenAutoGroupValue)
}

func IsMarketplaceRoutePoolTokenGroup(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), marketplacedomain.TokenGroupPrefix+"pool:")
}

func RoutePoolIDFromTokenGroup(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), marketplacedomain.TokenGroupPrefix+"pool:"))
}

func ResolveTokenGroupBinding(tokenGroup string, consumerUserID int) (*RoutingBinding, error) {
	if !IsMarketplaceTokenGroup(tokenGroup) {
		return nil, nil
	}
	if IsMarketplaceAutoTokenGroup(tokenGroup) {
		return nil, errors.New("第三方 Auto 分组需要在请求模型确定后解析")
	}
	if IsMarketplaceRoutePoolTokenGroup(tokenGroup) {
		return nil, errors.New("路由池需要在请求模型确定后解析")
	}
	groupID := strings.TrimPrefix(strings.TrimSpace(tokenGroup), marketplacedomain.TokenGroupPrefix)
	var group marketplaceschema.Group
	if err := platformdb.DB.First(&group, "id = ?", groupID).Error; err != nil {
		return nil, err
	}
	if !marketplacedomain.AcceptsTraffic(group.LifecycleStatus) || group.VerificationStatus != marketplacedomain.VerificationPassed {
		return nil, errors.New("市场分组当前不可用")
	}
	if !hasMarketplaceGroupAccessForGroup(&group, consumerUserID) {
		return nil, errors.New("市场分组未公开或无权访问")
	}
	var blocked int64
	if err := platformdb.DB.Model(&marketplaceschema.ChannelUserBlock{}).Where("channel_id = ? AND user_id = ?", group.ChannelID, consumerUserID).Count(&blocked).Error; err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") && !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
			return nil, err
		}
	}
	if blocked > 0 {
		return nil, errors.New("您已被该渠道主拉黑，无法使用此渠道")
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Select("model_prices", "declared_models").First(&channel, "id = ?", group.ChannelID).Error; err != nil {
		return nil, err
	}
	// Owner approved bargains and per-user overrides are stored separately
	// from the public group multiplier. Apply the override at bind time so the
	// approved rate is used by billing and routing immediately.
	var userMultiplier marketplaceschema.UserMultiplier
	if err := platformdb.DB.Where("channel_id = ? AND user_id = ?", group.ChannelID, consumerUserID).First(&userMultiplier).Error; err == nil && userMultiplier.Multiplier > 0 {
		group.Multiplier = userMultiplier.Multiplier
	}
	return &RoutingBinding{
		GroupID: group.ID, InternalGroup: group.InternalGroupName, OwnerUserID: group.OwnerUserID,
		SourceType: group.SourceType, CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: group.Multiplier,
		ModelPrices: decodeChannelModelPrices(channel.ModelPrices),
		Models:      decodeModels(channel.DeclaredModels),
	}, nil
}

func BindTokenToMarketplaceGroup(consumerUserID, tokenID int, groupID string) error {
	_, err := BindTokenToMarketplaceGroupResult(consumerUserID, tokenID, groupID)
	return err
}

// BindTokenToMarketplaceGroupResult binds an existing token, or creates one
// for the current user when tokenID is zero.
func BindTokenToMarketplaceGroupResult(consumerUserID, tokenID int, groupID string) (int, error) {
	groupValue := TokenGroupValue(groupID)
	binding, err := ResolveTokenGroupBinding(groupValue, consumerUserID)
	if err != nil {
		return 0, err
	}
	if binding == nil {
		return 0, errors.New("市场分组不存在")
	}
	var token *identityschema.Token
	if tokenID > 0 {
		token, err = identityapp.GetUserToken(consumerUserID, tokenID)
		if err != nil {
			return 0, err
		}
	} else {
		key, generateErr := platformruntime.GenerateKey()
		if generateErr != nil {
			return 0, generateErr
		}
		var group marketplaceschema.Group
		if err := platformdb.DB.First(&group, "id = ?", groupID).Error; err != nil {
			return 0, err
		}
		token = &identityschema.Token{
			UserId: consumerUserID, Name: "市场分组 - " + group.SystemDisplayName,
			Key: key, CreatedTime: platformruntime.GetTimestamp(), ExpiredTime: -1,
			UnlimitedQuota: true,
		}
	}
	token.Group = groupValue
	token.CrossGroupRetry = false
	if tokenID > 0 {
		return token.Id, identityapp.UpdateUserToken(token)
	}
	if err := identityapp.InsertUserToken(token); err != nil {
		return 0, err
	}
	return token.Id, nil
}
