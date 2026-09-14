package app

import (
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"golang.org/x/sync/singleflight"
	"sort"
	"strings"
	"sync"
	"time"
)

type SelfProfileResponse struct {
	Id              int            `json:"id"`
	ExternalId      string         `json:"external_id"`
	Username        string         `json:"username"`
	DisplayName     string         `json:"display_name"`
	Role            int            `json:"role"`
	Status          int            `json:"status"`
	Email           string         `json:"email"`
	PasswordSet     bool           `json:"password_set"`
	GitHubId        string         `json:"github_id"`
	DiscordId       string         `json:"discord_id"`
	OidcId          string         `json:"oidc_id"`
	WeChatId        string         `json:"wechat_id"`
	TelegramId      string         `json:"telegram_id"`
	Group           string         `json:"group"`
	Quota           int            `json:"quota"`
	UsedQuota       int            `json:"used_quota"`
	RequestCount    int            `json:"request_count"`
	AffCode         string         `json:"aff_code"`
	AffCount        int            `json:"aff_count"`
	AffQuota        int            `json:"aff_quota"`
	AffHistoryQuota int            `json:"aff_history_quota"`
	InviterId       int            `json:"inviter_id"`
	LinuxDOId       string         `json:"linux_do_id"`
	Setting         string         `json:"setting"`
	StripeCustomer  string         `json:"stripe_customer"`
	SidebarModules  string         `json:"sidebar_modules"`
	Permissions     map[string]any `json:"permissions"`
}

// The profile endpoint is called by several layout components at startup.
// Keep a very short cache and coalesce concurrent loads so one browser tab
// cannot fan out the expensive ledger aggregation into many identical queries.
var selfProfileCache struct {
	sync.RWMutex
	items map[string]cachedSelfProfile
}
var selfProfileLoads singleflight.Group

type cachedSelfProfile struct {
	profile *SelfProfileResponse
	at      time.Time
}

const selfProfileCacheTTL = 5 * time.Second

// GetSelfProfile loads the authenticated user's profile payload for /api/user/self.
func GetSelfProfile(userID int, userRole int) (*SelfProfileResponse, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	cacheKey := fmt.Sprintf("%d:%d", userID, userRole)
	selfProfileCache.RLock()
	if item, ok := selfProfileCache.items[cacheKey]; ok && time.Since(item.at) < selfProfileCacheTTL {
		selfProfileCache.RUnlock()
		return item.profile, nil
	}
	selfProfileCache.RUnlock()
	value, err, _ := selfProfileLoads.Do(cacheKey, func() (any, error) {
		selfProfileCache.RLock()
		if item, ok := selfProfileCache.items[cacheKey]; ok && time.Since(item.at) < selfProfileCacheTTL {
			selfProfileCache.RUnlock()
			return item.profile, nil
		}
		selfProfileCache.RUnlock()
		profile, loadErr := loadSelfProfile(userID, userRole)
		if loadErr != nil {
			return nil, loadErr
		}
		selfProfileCache.Lock()
		if selfProfileCache.items == nil {
			selfProfileCache.items = make(map[string]cachedSelfProfile)
		}
		selfProfileCache.items[cacheKey] = cachedSelfProfile{profile: profile, at: time.Now()}
		selfProfileCache.Unlock()
		return profile, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*SelfProfileResponse), nil
}

func loadSelfProfile(userID int, userRole int) (*SelfProfileResponse, error) {
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	walletQuota, err := loadDisplayWalletQuota(user)
	if err != nil {
		return nil, err
	}
	historicalUsedQuota, err := billingapp.GetUserHistoricalUsedQuotaForDisplay(user.Id, user.UsedQuota)
	if err != nil {
		return nil, err
	}

	// Clear admin remarks before exposing the user payload.
	user.Remark = ""
	userSetting := identitydomain.GetSetting(user)

	return &SelfProfileResponse{
		Id:              user.Id,
		ExternalId:      user.ExternalId,
		Username:        user.Username,
		DisplayName:     user.DisplayName,
		Role:            user.Role,
		Status:          user.Status,
		Email:           user.Email,
		PasswordSet:     user.Password != "",
		GitHubId:        user.GitHubId,
		DiscordId:       user.DiscordId,
		OidcId:          user.OidcId,
		WeChatId:        user.WeChatId,
		TelegramId:      user.TelegramId,
		Group:           user.Group,
		Quota:           walletQuota,
		UsedQuota:       historicalUsedQuota,
		RequestCount:    user.RequestCount,
		AffCode:         user.AffCode,
		AffCount:        user.AffCount,
		AffQuota:        user.AffQuota,
		AffHistoryQuota: user.AffHistoryQuota,
		InviterId:       user.InviterId,
		LinuxDOId:       user.LinuxDOId,
		Setting:         user.Setting,
		StripeCustomer:  user.StripeCustomer,
		SidebarModules:  userSetting.SidebarModules,
		Permissions:     calculateUserPermissions(userRole),
	}, nil
}

func loadDisplayWalletQuota(user *identityschema.User) (int, error) {
	if user == nil {
		return 0, nil
	}
	return billingapp.GetUserClaudeWalletQuota(user.Id)
}

// ListUserModels returns the authenticated user's deduplicated model catalog.
func ListUserModels(userID int) ([]string, error) {
	return ListUserModelsForGroup(userID, "")
}

// ListUserModelsForGroup returns the models routeable through one concrete
// group or through the user's configured auto-group chain.
func ListUserModelsForGroup(userID int, requestedGroup string) ([]string, error) {
	user, err := LoadUserCacheSnapshot(userID)
	if err != nil {
		return nil, err
	}

	usableGroups := gatewayroutingapp.GetUserUsableGroups(user.Group)
	groupNames := make([]string, 0, len(usableGroups))
	requestedGroup = strings.TrimSpace(requestedGroup)
	if requestedGroup == "" {
		for groupName := range usableGroups {
			if groupName != gatewayroutingapp.AutoGroupName {
				groupNames = append(groupNames, groupName)
			}
		}
	} else {
		requestedGroup = gatewayroutingapp.NormalizeTokenGroup(requestedGroup)
		if resolvedGroup, found, resolveErr := gatewayroutingapp.ResolveUserGroupAlias(requestedGroup); resolveErr != nil {
			return nil, resolveErr
		} else if found {
			requestedGroup = resolvedGroup
		}
		if requestedGroup == gatewayroutingapp.AutoGroupName {
			groupNames = gatewayroutingapp.GetUserAutoGroup(user.Group)
		} else {
			if _, ok := usableGroups[requestedGroup]; !ok {
				return nil, fmt.Errorf("group %s is not available for current user", requestedGroup)
			}
			groupNames = append(groupNames, requestedGroup)
		}
	}

	modelSet := make(map[string]struct{})
	for _, groupName := range groupNames {
		for _, modelName := range gatewayroutingapp.EnabledModelsForGroup(groupName) {
			modelSet[modelName] = struct{}{}
		}
	}

	models := make([]string, 0, len(modelSet))
	for modelName := range modelSet {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models, nil
}

// EnsureAffiliateCode returns the user's affiliate code, creating one when absent.
func EnsureAffiliateCode(userID int) (string, error) {
	user, err := LoadUserByID(userID, true)
	if err != nil {
		return "", err
	}
	if user.AffCode == "" {
		user.AffCode = platformruntime.GetRandomString(4)
		if err := identitystore.UpdateUser(user, false); err != nil {
			return "", err
		}
	}
	return user.AffCode, nil
}

// LoadAffiliateRewardsOverview ensures affiliate identity state and returns the overview payload.
func LoadAffiliateRewardsOverview(userID int) (*AffiliateRewardsOverview, error) {
	if _, err := EnsureAffiliateCode(userID); err != nil {
		return nil, err
	}
	return GetAffiliateRewardsOverview(userID)
}

func calculateUserPermissions(userRole int) map[string]any {
	permissions := map[string]any{}

	if userRole == constant.RoleRootUser {
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]any{}
		return permissions
	}

	if userRole == constant.RoleAdminUser {
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": map[string]any{
				"setting": false,
			},
		}
		return permissions
	}

	permissions["sidebar_settings"] = true
	permissions["sidebar_modules"] = map[string]any{
		"admin": false,
	}
	return permissions
}
