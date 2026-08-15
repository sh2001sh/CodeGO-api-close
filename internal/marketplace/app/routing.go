package app

import (
	"errors"
	"strings"

	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

func TokenGroupValue(groupID string) string {
	return marketplacedomain.TokenGroupPrefix + strings.TrimSpace(groupID)
}

func IsMarketplaceTokenGroup(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), marketplacedomain.TokenGroupPrefix)
}

func IsMarketplaceAutoTokenGroup(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), marketplacedomain.TokenAutoGroupValue)
}

func ResolveTokenGroupBinding(tokenGroup string, consumerUserID int) (*RoutingBinding, error) {
	if !IsMarketplaceTokenGroup(tokenGroup) {
		return nil, nil
	}
	if IsMarketplaceAutoTokenGroup(tokenGroup) {
		return nil, errors.New("第三方 Auto 分组需要在请求模型确定后解析")
	}
	groupID := strings.TrimPrefix(strings.TrimSpace(tokenGroup), marketplacedomain.TokenGroupPrefix)
	var group marketplaceschema.Group
	if err := platformdb.DB.First(&group, "id = ?", groupID).Error; err != nil {
		return nil, err
	}
	if !marketplacedomain.AcceptsTraffic(group.LifecycleStatus) || group.VerificationStatus != marketplacedomain.VerificationPassed {
		return nil, errors.New("市场分组当前不可用")
	}
	if group.OwnerUserID != consumerUserID && group.Visibility != marketplacedomain.VisibilityPublic {
		return nil, errors.New("市场分组未公开或无权访问")
	}
	return &RoutingBinding{
		GroupID: group.ID, InternalGroup: group.InternalGroupName, OwnerUserID: group.OwnerUserID,
		SourceType: group.SourceType, CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: group.Multiplier,
	}, nil
}

func BindTokenToMarketplaceGroup(consumerUserID, tokenID int, groupID string) error {
	groupValue := TokenGroupValue(groupID)
	binding, err := ResolveTokenGroupBinding(groupValue, consumerUserID)
	if err != nil {
		return err
	}
	if binding == nil {
		return errors.New("市场分组不存在")
	}
	token, err := identityapp.GetUserToken(consumerUserID, tokenID)
	if err != nil {
		return err
	}
	token.Group = groupValue
	token.CrossGroupRetry = false
	return identityapp.UpdateUserToken(token)
}
