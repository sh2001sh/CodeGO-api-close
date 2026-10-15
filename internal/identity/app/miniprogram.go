package app

import (
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"errors"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"strconv"
	"strings"
	"time"
)

const miniProgramBindCodeTTL = 10 * time.Minute

// BuildMiniProgramBindCodePayload creates a one-time mini program bind code for the current website user.
func BuildMiniProgramBindCodePayload(userID int, clientIP string) (map[string]any, error) {
	code, record, err := createMiniProgramBindCode(userID, clientIP, miniProgramBindCodeTTL)
	if err != nil {
		return nil, err
	}
	auditapp.RecordLog(userID, auditschema.LogTypeManage, "generated a mini program bind code")
	return map[string]any{
		"code":        code,
		"expires_at":  record.ExpiresAt,
		"ttl_seconds": getMiniProgramBindCodeTTLSeconds(record),
	}, nil
}

// BuildMiniProgramBindingPayload returns the current website user's active mini program binding.
func BuildMiniProgramBindingPayload(userID int) (map[string]any, error) {
	binding, err := getActiveMiniProgramBindingByUserID(userID)
	if err != nil {
		return nil, err
	}
	user, err := currentMiniProgramUser(userID)
	if err != nil {
		return nil, err
	}
	return miniProgramBindingPayload(binding, user), nil
}

// DeleteMiniProgramBinding revokes the current website user's active mini program binding.
func DeleteMiniProgramBinding(userID int) error {
	if err := revokeMiniProgramBindingByUserID(userID); err != nil {
		return err
	}
	auditapp.RecordLog(userID, auditschema.LogTypeManage, "revoked the mini program binding")
	return nil
}

// BuildMiniProgramSessionResponse exchanges a login code and returns the signed mini program session response.
func BuildMiniProgramSessionResponse(code string) (map[string]any, error) {
	openID, unionID, err := ExchangeMiniProgramCode(code)
	if err != nil {
		return nil, err
	}

	binding, err := getActiveMiniProgramBindingByOpenID(openID)
	if err != nil {
		return nil, err
	}

	boundUserID := 0
	if binding != nil {
		boundUserID = binding.UserId
	}
	token, expiresAt, err := BuildMiniProgramSessionToken(openID, unionID, boundUserID)
	if err != nil {
		return nil, err
	}

	state, err := buildMiniProgramSessionState(openID)
	if err != nil {
		return nil, err
	}
	state["token"] = token
	state["expires_at"] = expiresAt
	return state, nil
}

// BuildMiniProgramMeResponse returns binding state and website guidance for the current mini program session.
func BuildMiniProgramMeResponse(openID string, expiresAt int64) (map[string]any, error) {
	state, err := buildMiniProgramSessionState(openID)
	if err != nil {
		return nil, err
	}
	state["expires_at"] = expiresAt
	return state, nil
}

// BindMiniProgramSession binds the current mini program session to a website account using a one-time code.
func BindMiniProgramSession(bindCode string, openID string, unionID string) (map[string]any, error) {
	_, binding, err := consumeMiniProgramBindCodeAndBind(bindCode, openID, unionID)
	if err != nil {
		return nil, err
	}

	user, err := LoadUserByID(binding.UserId, false)
	if err != nil {
		return nil, err
	}
	auditapp.RecordLog(binding.UserId, auditschema.LogTypeManage, "bound a mini program account")
	return miniProgramBindingPayload(binding, user), nil
}

// UnbindMiniProgramSession revokes the current mini program binding from the mini program side.
func UnbindMiniProgramSession(userID int, openID string) error {
	if err := revokeMiniProgramBindingByOpenID(openID); err != nil {
		return err
	}
	if userID > 0 {
		auditapp.RecordLog(userID, auditschema.LogTypeManage, "unbound the mini program account")
	}
	return nil
}

// BuildMiniProgramShareCheck returns sensitive-word scan results for share content.
func BuildMiniProgramShareCheck(title string, content string) (map[string]any, error) {
	text := strings.TrimSpace(strings.Join([]string{title, content}, "\n"))
	if text == "" {
		return nil, errors.New("content is required")
	}
	containsSensitive, words := CheckSensitiveText(text)
	return map[string]any{
		"safe":      !containsSensitive,
		"keywords":  words,
		"can_share": !containsSensitive,
	}, nil
}

func currentMiniProgramUser(userID int) (*identityschema.User, error) {
	return getMiniProgramUserByID(userID)
}

func buildMiniProgramSessionState(openID string) (map[string]any, error) {
	binding, err := getActiveMiniProgramBindingByOpenID(openID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return map[string]any{
			"bound":         false,
			"openid_masked": maskMiniProgramIdentifier(openID),
			"binding": map[string]any{
				"bound": false,
			},
			"website": buildMiniProgramWebsiteLinks(),
		}, nil
	}

	user, err := LoadUserByID(binding.UserId, false)
	if err != nil || user == nil || user.Status != constant.UserStatusEnabled {
		return map[string]any{
			"bound":         false,
			"openid_masked": maskMiniProgramIdentifier(openID),
			"binding": map[string]any{
				"bound": false,
			},
			"website": buildMiniProgramWebsiteLinks(),
		}, nil
	}

	return map[string]any{
		"bound":         true,
		"openid_masked": maskMiniProgramIdentifier(openID),
		"binding":       miniProgramBindingPayload(binding, user),
		"website":       buildMiniProgramWebsiteLinks(),
	}, nil
}

func maskMiniProgramIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:2]) + strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-2:])
}

func maskMiniProgramAccountLabel(user *identityschema.User) string {
	if user == nil {
		return ""
	}
	if strings.TrimSpace(user.Email) != "" {
		return platformtext.MaskEmail(user.Email)
	}
	if strings.TrimSpace(user.DisplayName) != "" {
		return maskMiniProgramIdentifier(user.DisplayName)
	}
	return maskMiniProgramIdentifier(user.Username)
}

func buildMiniProgramWebsiteLinks() map[string]any {
	baseURL := strings.TrimRight(strings.TrimSpace(CallbackAddress()), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(platformconfig.TopUpLink), "/")
	}

	link := func(path string) string {
		if baseURL == "" {
			return path
		}
		return baseURL + path
	}

	return map[string]any{
		"home_url":     link("/"),
		"landing_url":  link("/miniapp/landing"),
		"guide_url":    link("/guide"),
		"pricing_url":  link("/pricing"),
		"profile_url":  link("/profile"),
		"packages_url": link("/packages"),
		"support_hint": "Use the website for account actions, purchases, and support requests.",
	}
}

func miniProgramBindingPayload(binding *identitydomain.UserWeChatBinding, user *identityschema.User) map[string]any {
	if binding == nil {
		return map[string]any{
			"bound": false,
		}
	}

	payload := map[string]any{
		"bound":         true,
		"status":        binding.Status,
		"openid_masked": maskMiniProgramIdentifier(binding.OpenID),
		"bound_at":      binding.BoundAt,
		"last_seen_at":  binding.LastSeenAt,
	}
	if user != nil {
		payload["user_id"] = user.Id
		payload["account_masked"] = maskMiniProgramAccountLabel(user)
		payload["username_masked"] = maskMiniProgramIdentifier(user.Username)
	}
	return payload
}

func NormalizeMiniProgramWindowDays(raw string, fallback int) int {
	days, _ := strconv.Atoi(strings.TrimSpace(raw))
	if days <= 0 {
		days = fallback
	}
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}
	return days
}
