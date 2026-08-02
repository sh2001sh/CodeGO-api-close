package app

import (
	"errors"
	"github.com/samber/hot"
	"github.com/sh2001sh/new-api/constant"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/sh2001sh/new-api/internal/platform/cachex"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DesktopDefaultTokenPrefix = "Code Go Desktop - "
	DesktopToolCodex          = "codex"
	DesktopToolClaude         = "claude"
	DesktopToolGemini         = "gemini"
	DesktopToolOpenCode       = "opencode"
	DesktopToolOpenClaw       = "openclaw"
	DesktopToolHermes         = "hermes"
	desktopImportCodeTTL      = 10 * time.Minute
	desktopImportCacheNS      = "new-api:desktop_import:v1"
)

var (
	desktopImportCacheOnce sync.Once
	desktopImportCache     *cachex.HybridCache[DesktopImportConfigPayload]
)

type DesktopEnsureTokenRequest struct {
	DeviceName string `json:"device_name"`
	Group      string `json:"group"`
}

type DesktopEnsureTokenResponse struct {
	Token     *identityschema.Token `json:"token"`
	Created   bool                  `json:"created"`
	FullKey   string                `json:"full_key"`
	TokenName string                `json:"token_name"`
}

type DesktopUpdateTokenGroupRequest struct {
	Group string `json:"group"`
}

type DesktopGroupItem struct {
	Name                 string `json:"name"`
	Desc                 string `json:"desc"`
	Ratio                any    `json:"ratio"`
	Current              bool   `json:"current"`
	AvailableModelsCount int    `json:"available_models_count"`
}

type DesktopGroupsResponse struct {
	Current string             `json:"current"`
	Items   []DesktopGroupItem `json:"items"`
}

// NormalizeDesktopTool maps accepted desktop client aliases to canonical tool ids.
func NormalizeDesktopTool(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case DesktopToolCodex:
		return DesktopToolCodex
	case DesktopToolClaude, "claude-code":
		return DesktopToolClaude
	case DesktopToolGemini, "gemini-cli":
		return DesktopToolGemini
	case DesktopToolOpenCode, "open-code":
		return DesktopToolOpenCode
	case DesktopToolOpenClaw, "open-claw":
		return DesktopToolOpenClaw
	case DesktopToolHermes, "hermes-agent":
		return DesktopToolHermes
	default:
		return ""
	}
}

// BuildDesktopTokenName builds the deterministic desktop token name for a device.
func BuildDesktopTokenName(deviceName string) string {
	name := strings.TrimSpace(deviceName)
	if name == "" {
		return DesktopDefaultTokenPrefix + "Default"
	}
	return DesktopDefaultTokenPrefix + name
}

// NormalizeDesktopServerAddress returns the desktop-visible server base URL.
func NormalizeDesktopServerAddress(raw string) string {
	return platformruntime.NormalizeDesktopServerAddress(raw)
}

// DesktopServiceMaintenanceEnabled reports whether desktop maintenance mode is enabled.
func DesktopServiceMaintenanceEnabled(options map[string]string) bool {
	if options == nil {
		return false
	}

	raw := strings.TrimSpace(options["Maintenance"])
	if raw == "" {
		raw = strings.TrimSpace(options["DesktopMaintenance"])
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func quotaToUSD(quota int) float64 {
	return float64(quota) / platformruntime.QuotaPerUnit
}

func getDesktopImportCache() *cachex.HybridCache[DesktopImportConfigPayload] {
	desktopImportCacheOnce.Do(func() {
		desktopImportCache = cachex.NewHybridCache[DesktopImportConfigPayload](cachex.HybridCacheConfig[DesktopImportConfigPayload]{
			Namespace: cachex.Namespace(desktopImportCacheNS),
			Redis:     platformcache.RDB,
			RedisEnabled: func() bool {
				return platformcache.RedisEnabled && platformcache.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[DesktopImportConfigPayload]{},
			Memory: func() *hot.HotCache[string, DesktopImportConfigPayload] {
				return hot.NewHotCache[string, DesktopImportConfigPayload](hot.LRU, 512).
					WithTTL(desktopImportCodeTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return desktopImportCache
}

// PurgeDesktopImportCacheForTest clears the desktop import cache between tests.
func PurgeDesktopImportCacheForTest() error {
	if desktopImportCache == nil {
		return nil
	}
	return desktopImportCache.Purge()
}

// FindDesktopToken loads a desktop token by owner and token name.
func FindDesktopToken(userID int, tokenName string) (*identityschema.Token, error) {
	var token identityschema.Token
	err := platformdb.DB.Where("user_id = ? AND name = ?", userID, tokenName).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// FindDesktopTokenByID loads a desktop token by owner and token id.
func FindDesktopTokenByID(userID int, tokenID int) (*identityschema.Token, error) {
	return GetUserToken(userID, tokenID)
}

// ValidateDesktopGroupForUser verifies the requested desktop group is usable by the user.
func ValidateDesktopGroupForUser(user *identityschema.User, requested string) (string, error) {
	userGroup := strings.TrimSpace(user.Group)
	if userGroup == "" {
		userGroup = "default"
	}
	group := gatewayroutingapp.NormalizeTokenGroup(requested)
	if group == gatewayroutingapp.AutoGroupName {
		if len(gatewayroutingapp.GetUserAutoGroup(userGroup)) == 0 {
			return "", errors.New("auto group is not available for current user")
		}
		return group, nil
	}
	if _, ok := gatewayroutingapp.GetUserUsableGroups(userGroup)[group]; !ok {
		return "", errors.New("group is not available for current user")
	}
	return group, nil
}

// ListDesktopAvailableModels returns all enabled models visible to the given group.
func ListDesktopAvailableModels(group string) []string {
	groups := gatewayroutingapp.GetUserUsableGroups(group)
	groupNames := make([]string, 0, len(groups))
	for usableGroup := range groups {
		groupNames = append(groupNames, usableGroup)
	}
	return listDesktopAvailableModelsForGroups(groupNames)
}

// ListDesktopAvailableModelsForTokenGroup returns models available to a token's effective group.
func ListDesktopAvailableModelsForTokenGroup(userGroup string, tokenGroup string) []string {
	userGroup = strings.TrimSpace(userGroup)
	if userGroup == "" {
		userGroup = "default"
	}
	tokenGroup = gatewayroutingapp.NormalizeTokenGroup(tokenGroup)
	if tokenGroup == gatewayroutingapp.AutoGroupName {
		autoGroups := gatewayroutingapp.GetUserAutoGroup(userGroup)
		if len(autoGroups) > 0 {
			return listDesktopAvailableModelsForGroups(autoGroups)
		}
	}
	return listDesktopAvailableModelsForGroups([]string{tokenGroup})
}

// ListDesktopGroups builds the desktop group selector payload for a user.
func ListDesktopGroups(user *identityschema.User) DesktopGroupsResponse {
	currentGroup := strings.TrimSpace(user.Group)
	if currentGroup == "" {
		currentGroup = "default"
	}

	usableGroups := gatewayroutingapp.GetUserUsableGroups(currentGroup)
	groupNames := make([]string, 0, len(usableGroups))
	for groupName := range usableGroups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	ratios := gatewaystore.GetGroupRatioCopy()
	items := make([]DesktopGroupItem, 0, len(groupNames))
	for _, groupName := range groupNames {
		ratioValue := any(gatewayroutingapp.GetUserGroupRatio(currentGroup, groupName))
		if groupName == "auto" {
			ratioValue = "自动"
		} else if _, ok := ratios[groupName]; !ok {
			ratioValue = gatewayroutingapp.GetUserGroupRatio(currentGroup, groupName)
		}
		items = append(items, DesktopGroupItem{
			Name:                 groupName,
			Desc:                 usableGroups[groupName],
			Ratio:                ratioValue,
			Current:              groupName == currentGroup,
			AvailableModelsCount: len(ListDesktopAvailableModels(groupName)),
		})
	}

	return DesktopGroupsResponse{
		Current: currentGroup,
		Items:   items,
	}
}

// EnsureDesktopToken returns the named desktop token or creates it when missing.
func EnsureDesktopToken(userID int, req DesktopEnsureTokenRequest) (*DesktopEnsureTokenResponse, error) {
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	group, err := ValidateDesktopGroupForUser(user, req.Group)
	if err != nil {
		return nil, err
	}

	tokenName := BuildDesktopTokenName(req.DeviceName)
	if len(tokenName) > 50 {
		return nil, errors.New("desktop token name is too long")
	}

	existing, err := FindDesktopToken(userID, tokenName)
	if err == nil && existing != nil {
		return &DesktopEnsureTokenResponse{
			Token:     BuildMaskedTokenResponse(existing),
			Created:   false,
			FullKey:   existing.GetFullKey(),
			TokenName: existing.Name,
		}, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	key, err := platformruntime.GenerateKey()
	if err != nil {
		return nil, err
	}

	token := &identityschema.Token{
		UserId:             userID,
		Name:               tokenName,
		Key:                key,
		Status:             constant.TokenStatusEnabled,
		CreatedTime:        platformruntime.GetTimestamp(),
		AccessedTime:       platformruntime.GetTimestamp(),
		ExpiredTime:        -1,
		RemainQuota:        0,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: false,
		Group:              group,
	}
	if err := InsertUserToken(token); err != nil {
		return nil, err
	}

	return &DesktopEnsureTokenResponse{
		Token:     BuildMaskedTokenResponse(token),
		Created:   true,
		FullKey:   token.GetFullKey(),
		TokenName: token.Name,
	}, nil
}

// UpdateDesktopTokenGroup updates the selected group on a desktop token.
func UpdateDesktopTokenGroup(userID int, tokenID int, req DesktopUpdateTokenGroupRequest) (*identityschema.Token, error) {
	user, err := LoadUserByID(userID, false)
	if err != nil {
		return nil, err
	}
	group, err := ValidateDesktopGroupForUser(user, req.Group)
	if err != nil {
		return nil, err
	}

	token, err := FindDesktopTokenByID(userID, tokenID)
	if err != nil {
		return nil, err
	}
	token.Group = group
	if err = UpdateUserToken(token); err != nil {
		return nil, err
	}
	return BuildMaskedTokenResponse(token), nil
}

func listDesktopAvailableModelsForGroups(groupNames []string) []string {
	modelSet := make(map[string]struct{})
	models := make([]string, 0)
	for _, usableGroup := range groupNames {
		var groupModels []string
		_ = platformdb.DB.Table("abilities").
			Where(map[string]any{
				"group":   usableGroup,
				"enabled": true,
			}).
			Distinct("model").
			Pluck("model", &groupModels).Error
		for _, modelName := range groupModels {
			if _, ok := modelSet[modelName]; ok {
				continue
			}
			modelSet[modelName] = struct{}{}
			models = append(models, modelName)
		}
	}
	sort.Strings(models)
	return models
}

func pickDesktopRecommendedModel(models []string, prefixes ...string) string {
	for _, prefix := range prefixes {
		for _, modelName := range models {
			if strings.HasPrefix(modelName, prefix) {
				return modelName
			}
		}
	}
	return ""
}

func desktopProviderDisplayName(tool string) string {
	switch tool {
	case DesktopToolCodex:
		return "Code Go Codex"
	case DesktopToolClaude:
		return "Code Go Claude"
	case DesktopToolGemini:
		return "Code Go Gemini"
	case DesktopToolOpenCode:
		return "Code Go OpenCode"
	case DesktopToolOpenClaw:
		return "Code Go OpenClaw"
	case DesktopToolHermes:
		return "Code Go Hermes"
	default:
		return "Code Go"
	}
}

func desktopToolIcon(tool string) string {
	switch tool {
	case DesktopToolCodex, DesktopToolClaude, DesktopToolGemini, DesktopToolOpenCode,
		DesktopToolOpenClaw, DesktopToolHermes:
		return "codego"
	default:
		return ""
	}
}

func strconvQuote(value string) string {
	return strconv.Quote(value)
}
