package app

import (
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"sort"
)

type SelfProfileResponse struct {
	Id              int            `json:"id"`
	ExternalId      string         `json:"external_id"`
	Username        string         `json:"username"`
	DisplayName     string         `json:"display_name"`
	Role            int            `json:"role"`
	Status          int            `json:"status"`
	Email           string         `json:"email"`
	GitHubId        string         `json:"github_id"`
	DiscordId       string         `json:"discord_id"`
	OidcId          string         `json:"oidc_id"`
	WeChatId        string         `json:"wechat_id"`
	TelegramId      string         `json:"telegram_id"`
	Group           string         `json:"group"`
	Quota           int            `json:"quota"`
	ClaudeQuota     int            `json:"claude_quota"`
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

// GetSelfProfile loads the authenticated user's profile payload for /api/user/self.
func GetSelfProfile(userID int, userRole int) (*SelfProfileResponse, error) {
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	walletQuota, claudeWalletQuota, err := loadDisplayWalletQuotas(user)
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
		GitHubId:        user.GitHubId,
		DiscordId:       user.DiscordId,
		OidcId:          user.OidcId,
		WeChatId:        user.WeChatId,
		TelegramId:      user.TelegramId,
		Group:           user.Group,
		Quota:           walletQuota,
		ClaudeQuota:     claudeWalletQuota,
		UsedQuota:       user.UsedQuota,
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

func loadDisplayWalletQuotas(user *identityschema.User) (int, int, error) {
	if user == nil {
		return 0, 0, nil
	}
	walletQuota, err := billingapp.GetUserWalletQuota(user.Id)
	if err != nil {
		return 0, 0, err
	}
	claudeWalletQuota, err := billingapp.GetUserClaudeWalletQuota(user.Id)
	if err != nil {
		return 0, 0, err
	}
	return walletQuota, claudeWalletQuota, nil
}

// ListUserModels returns the authenticated user's deduplicated model catalog.
func ListUserModels(userID int) ([]string, error) {
	user, err := LoadUserCacheSnapshot(userID)
	if err != nil {
		return nil, err
	}

	modelSet := make(map[string]struct{})
	for groupName := range gatewayroutingapp.GetUserUsableGroups(user.Group) {
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
