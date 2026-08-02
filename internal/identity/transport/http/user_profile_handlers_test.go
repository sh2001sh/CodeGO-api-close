package http

import (
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"testing"
)

func TestGetUserSelfReturnsProfilePermissionsAndSidebarModules(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{
		Id:             1,
		ExternalId:     "7KM4QZ",
		Username:       "profile-user",
		Password:       "password123",
		DisplayName:    "Profile User",
		Role:           constant.RoleAdminUser,
		Status:         constant.UserStatusEnabled,
		Email:          "profile@example.com",
		Group:          "default",
		Quota:          42,
		ClaudeQuota:    7,
		UsedQuota:      5,
		RequestCount:   11,
		InviterId:      99,
		Setting:        "",
		StripeCustomer: "cus_profile",
	}
	identitydomain.SetSetting(user, dto.UserSetting{SidebarModules: `{"chat":{"enabled":true}}`})
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, "GET", "/api/user/self", nil, user.Id)
	ctx.Set("role", constant.RoleAdminUser)
	GetUserSelf(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var payload struct {
		Id             int            `json:"id"`
		ExternalId     string         `json:"external_id"`
		Username       string         `json:"username"`
		DisplayName    string         `json:"display_name"`
		SidebarModules string         `json:"sidebar_modules"`
		Permissions    map[string]any `json:"permissions"`
		StripeCustomer string         `json:"stripe_customer"`
		InviterId      int            `json:"inviter_id"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode self profile: %v", err)
	}

	if payload.Id != user.Id || payload.Username != user.Username || payload.DisplayName != user.DisplayName {
		t.Fatalf("unexpected self profile payload: %#v", payload)
	}
	if payload.ExternalId != user.ExternalId {
		t.Fatalf("expected public user ID %q, got %q", user.ExternalId, payload.ExternalId)
	}
	if payload.SidebarModules != `{"chat":{"enabled":true}}` {
		t.Fatalf("expected sidebar modules to be extracted, got %q", payload.SidebarModules)
	}
	if payload.StripeCustomer != user.StripeCustomer || payload.InviterId != user.InviterId {
		t.Fatalf("expected persisted profile fields, got %#v", payload)
	}
	if sidebarSettings, ok := payload.Permissions["sidebar_settings"].(bool); !ok || !sidebarSettings {
		t.Fatalf("expected admin sidebar_settings permission, got %#v", payload.Permissions)
	}
	permissionModules, ok := payload.Permissions["sidebar_modules"].(map[string]any)
	if !ok {
		t.Fatalf("expected sidebar_modules permission map, got %#v", payload.Permissions["sidebar_modules"])
	}
	adminPermission, ok := permissionModules["admin"].(map[string]any)
	if !ok {
		t.Fatalf("expected admin permission map, got %#v", permissionModules["admin"])
	}
	if settingPermission, ok := adminPermission["setting"].(bool); !ok || settingPermission {
		t.Fatalf("expected admin setting permission to be false, got %#v", adminPermission["setting"])
	}
}

func TestGetUserModelsReturnsSortedDeduplicatedModels(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{Id: 1, Username: "models-user", Password: "password123", DisplayName: "Models User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	abilities := []gatewayschema.Ability{
		{Group: "default", Model: "gpt-5", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "claude-3-7-sonnet", ChannelId: 2, Enabled: true},
		{Group: "default", Model: "gpt-5", ChannelId: 3, Enabled: true},
	}
	for _, ability := range abilities {
		record := ability
		if err := db.Create(&record).Error; err != nil {
			t.Fatalf("failed to seed ability: %v", err)
		}
	}

	ctx, recorder := newAuthenticatedContext(t, "GET", "/api/user/models", nil, user.Id)
	GetUserModels(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var models []string
	if err := platformencoding.Unmarshal(response.Data, &models); err != nil {
		t.Fatalf("failed to decode user models: %v", err)
	}
	expected := []string{"claude-3-7-sonnet", "gpt-5"}
	if len(models) != len(expected) {
		t.Fatalf("expected %d models, got %#v", len(expected), models)
	}
	for i := range expected {
		if models[i] != expected[i] {
			t.Fatalf("expected sorted deduplicated models %v, got %v", expected, models)
		}
	}
}

func TestGetUserModelsFiltersByAutoGroupChain(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		_ = gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups)
		_ = gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups)
	})
	if err := gatewaygroups.UpdateAutoGroupsByJsonString(`["default","claude"]`); err != nil {
		t.Fatalf("failed to configure auto groups: %v", err)
	}
	if err := gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"default":"默认","claude":"Claude","archive":"归档"}`); err != nil {
		t.Fatalf("failed to configure usable groups: %v", err)
	}

	user := &identityschema.User{Id: 1, Username: "auto-models-user", Password: "password123", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	for _, ability := range []gatewayschema.Ability{
		{Group: "default", Model: "gpt-5", ChannelId: 1, Enabled: true},
		{Group: "claude", Model: "claude-sonnet-4-5", ChannelId: 2, Enabled: true},
		{Group: "archive", Model: "legacy-model", ChannelId: 3, Enabled: true},
	} {
		record := ability
		if err := db.Create(&record).Error; err != nil {
			t.Fatalf("failed to seed ability: %v", err)
		}
	}

	ctx, recorder := newAuthenticatedContext(t, "GET", "/api/user/models?group=auto", nil, user.Id)
	GetUserModels(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var models []string
	if err := platformencoding.Unmarshal(response.Data, &models); err != nil {
		t.Fatalf("failed to decode user models: %v", err)
	}
	expected := []string{"claude-sonnet-4-5", "gpt-5"}
	if len(models) != len(expected) {
		t.Fatalf("expected auto models %v, got %v", expected, models)
	}
	for index := range expected {
		if models[index] != expected[index] {
			t.Fatalf("expected auto models %v, got %v", expected, models)
		}
	}
}

func TestGetUserAffiliateCodeCreatesAndPersistsCode(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{Id: 1, Username: "affiliate-user", Password: "password123", DisplayName: "Affiliate User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, "GET", "/api/user/aff", nil, user.Id)
	GetUserAffiliateCode(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var code string
	if err := platformencoding.Unmarshal(response.Data, &code); err != nil {
		t.Fatalf("failed to decode affiliate code: %v", err)
	}
	if len(code) != 4 {
		t.Fatalf("expected generated 4-char affiliate code, got %q", code)
	}

	reloaded, err := loadUserByIDForTest(user.Id, true)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.AffCode != code {
		t.Fatalf("expected affiliate code to persist, got %q want %q", reloaded.AffCode, code)
	}
}

func TestGetUserAffiliateRewardsOverviewReturnsInviteeStatuses(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	inviter := &identityschema.User{Id: 1, ExternalId: "INV001", Username: "inviter-user", Password: "password123", DisplayName: "Inviter User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	inviteeOne := &identityschema.User{Id: 2, ExternalId: "ITE001", Username: "invitee-1", Password: "password123", DisplayName: "Invitee One", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "INV1", InviterId: inviter.Id}
	inviteeTwo := &identityschema.User{Id: 3, ExternalId: "ITE002", Username: "invitee-2", Password: "password123", DisplayName: "Invitee Two", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "INV2", InviterId: inviter.Id}
	for _, user := range []*identityschema.User{inviter, inviteeOne, inviteeTwo} {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("failed to seed user: %v", err)
		}
	}
	if err := db.Create(&commerceschema.SubscriptionResetOpportunityAccount{
		UserId:         inviter.Id,
		EarnedTotal:    1,
		UsedTotal:      0,
		AvailableTotal: 1,
	}).Error; err != nil {
		t.Fatalf("failed to seed reset opportunity account: %v", err)
	}
	if err := db.Create(&commerceschema.SubscriptionResetOpportunityLedger{
		UserId:        inviter.Id,
		RelatedUserId: inviteeOne.Id,
		ChangeType:    commerceschema.SubscriptionResetOpportunityChangeEarn,
		Delta:         1,
		BalanceAfter:  1,
		SourceType:    "subscription",
		SourceRef:     "order-1",
		EventKey:      "affiliate-earn-1",
	}).Error; err != nil {
		t.Fatalf("failed to seed reset opportunity ledger: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, "GET", "/api/user/aff/overview", nil, inviter.Id)
	GetUserAffiliateRewardsOverview(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var overview identityapp.AffiliateRewardsOverview
	if err := platformencoding.Unmarshal(response.Data, &overview); err != nil {
		t.Fatalf("failed to decode affiliate overview: %v", err)
	}
	if len(overview.AffiliateCode) != 4 {
		t.Fatalf("expected generated affiliate code, got %q", overview.AffiliateCode)
	}
	if overview.InvitedCount != 2 || overview.SuccessfulPurchaseInvites != 1 {
		t.Fatalf("unexpected affiliate counts: %#v", overview)
	}
	if overview.ResetOpportunity.AvailableCount != 1 || overview.ResetOpportunity.EarnedTotal != 1 {
		t.Fatalf("unexpected reset opportunity summary: %#v", overview.ResetOpportunity)
	}

	statusByInvitee := make(map[int]identityapp.AffiliateInviteeRewardStatus, len(overview.Invitees))
	for _, invitee := range overview.Invitees {
		statusByInvitee[invitee.InviteeId] = invitee
	}
	if inviteeStatus, ok := statusByInvitee[inviteeOne.Id]; !ok || !inviteeStatus.MonthCardPurchased || !inviteeStatus.ResetOpportunityEarned {
		t.Fatalf("expected first invitee to be marked as rewarded, got %#v", inviteeStatus)
	}
	if inviteeStatus, ok := statusByInvitee[inviteeOne.Id]; !ok || inviteeStatus.InviteeExternalId != inviteeOne.ExternalId {
		t.Fatalf("expected invitee public ID, got %#v", inviteeStatus)
	}
	if inviteeStatus, ok := statusByInvitee[inviteeTwo.Id]; !ok || inviteeStatus.MonthCardPurchased || inviteeStatus.ResetOpportunityEarned {
		t.Fatalf("expected second invitee to be unrewarded, got %#v", inviteeStatus)
	}
}
