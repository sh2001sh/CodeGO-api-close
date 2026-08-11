package http

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditdomain "github.com/sh2001sh/new-api/internal/audit/domain"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"

	"gorm.io/gorm"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type desktopSummaryResponse struct {
	Account struct {
		ID                 int      `json:"id"`
		Username           string   `json:"username"`
		DisplayName        string   `json:"display_name"`
		QuotaUSD           float64  `json:"quota_usd"`
		ClaudeQuotaUSD     float64  `json:"claude_quota_usd"`
		UsedQuotaUSD       float64  `json:"used_quota_usd"`
		FundingSourceOrder []string `json:"funding_source_order"`
	} `json:"account"`
	Subscriptions []struct {
		PlanTitle      string  `json:"plan_title"`
		AmountTotalUSD float64 `json:"amount_total_usd"`
		AmountUsedUSD  float64 `json:"amount_used_usd"`
		RemainingUSD   float64 `json:"remaining_usd"`
		Unlimited      bool    `json:"unlimited"`
		EndTime        int64   `json:"end_time"`
	} `json:"subscriptions"`
	Tokens struct {
		Total        int                `json:"total"`
		DesktopToken *tokenResponseItem `json:"desktop_token"`
	} `json:"tokens"`
	Usage struct {
		AvailableModels []string `json:"available_models"`
		TodayUSD        float64  `json:"today_usd"`
		Last7DaysUSD    float64  `json:"last_7_days_usd"`
		LastRequestAt   int64    `json:"last_request_at"`
	} `json:"usage"`
	Service struct {
		Status            string   `json:"status"`
		Notice            string   `json:"notice"`
		Maintenance       bool     `json:"maintenance"`
		RecommendedAction string   `json:"recommended_action"`
		AffectedScopes    []string `json:"affected_scopes"`
	} `json:"service"`
	RecentLogs []struct {
		Content string `json:"content"`
	} `json:"recent_logs"`
	Actions struct {
		ServerAddress string `json:"server_address"`
		LogsPath      string `json:"logs_path"`
	} `json:"actions"`
}

type desktopEnsureTokenPayload struct {
	Token struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"token"`
	Created   bool   `json:"created"`
	FullKey   string `json:"full_key"`
	TokenName string `json:"token_name"`
}

type desktopTemplatePayload struct {
	Tool          string            `json:"tool"`
	Label         string            `json:"label"`
	Endpoint      string            `json:"endpoint"`
	AuthScheme    string            `json:"auth_scheme"`
	ModelFormat   string            `json:"model_format"`
	Env           map[string]string `json:"env"`
	ServerAddress string            `json:"server_address"`
}

type desktopImportCreatePayload struct {
	Code      string `json:"code"`
	DeepLink  string `json:"deep_link"`
	ConfigURL string `json:"config_url"`
	ExpiresIn int64  `json:"expires_in_seconds"`
	Tool      string `json:"tool"`
	TokenName string `json:"token_name"`
	Provider  string `json:"provider_name"`
}

type desktopImportConfigResponse struct {
	Tool         string `json:"tool"`
	Name         string `json:"name"`
	Homepage     string `json:"homepage"`
	Endpoint     string `json:"endpoint"`
	APIKey       string `json:"apiKey"`
	Model        string `json:"model"`
	HaikuModel   string `json:"haikuModel"`
	SonnetModel  string `json:"sonnetModel"`
	OpusModel    string `json:"opusModel"`
	Enabled      bool   `json:"enabled"`
	Config       string `json:"config"`
	ConfigFormat string `json:"configFormat"`
	Icon         string `json:"icon"`
}

type codexImportConfigBody struct {
	Auth   map[string]string `json:"auth"`
	Config string            `json:"config"`
}

func setupDesktopHTTPTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	platformcache.RedisEnabled = false

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	platformdb.DB = db
	platformdb.LogDB = db

	if err := db.AutoMigrate(
		&identityschema.User{},
		&identityschema.Token{},
		&auditschema.Log{},
		&auditdomain.QuotaData{},
		&gatewayschema.Ability{},
		&gatewayschema.Channel{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
		&identitydomain.ImageWorkspaceItem{},
		&identitydomain.Checkin{},
		&commerceschema.SubscriptionResetOpportunityAccount{},
		&commerceschema.SubscriptionResetOpportunityLedger{},
		&identitydomain.TwoFA{},
		&identitydomain.TwoFABackupCode{},
		&identitydomain.PasskeyCredential{},
		&identitydomain.CustomOAuthProvider{},
		&identitydomain.UserOAuthBinding{},
		&commerceschema.SubscriptionPlan{},
		&commerceschema.UserSubscription{},
		&commerceschema.BlindBoxOrder{},
		&identitydomain.DesktopAuthSession{},
		&identitydomain.DesktopAuthorizedDevice{},
		&identitydomain.DesktopDiagnosticReport{},
		&identitydomain.DesktopTelemetryEvent{},
	); err != nil {
		t.Fatalf("failed to migrate desktop http tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedDesktopToken(t *testing.T, db *gorm.DB, userID int, name string, rawKey string) *identityschema.Token {
	t.Helper()
	token := &identityschema.Token{
		UserId:         userID,
		Name:           name,
		Key:            rawKey,
		Status:         constant.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: true,
		Group:          "default",
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return token
}

func resetDesktopImportCacheForTest(t *testing.T) {
	t.Helper()
	if err := identityapp.PurgeDesktopImportCacheForTest(); err != nil {
		t.Fatalf("failed to purge desktop import cache: %v", err)
	}
}

func TestGetDesktopAccountSummaryReturnsAggregatedDesktopData(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{
		Id:           1,
		Username:     "desktop-user",
		Password:     "password123",
		DisplayName:  "Desktop User",
		Role:         constant.RoleCommonUser,
		Status:       constant.UserStatusEnabled,
		Group:        "default",
		Quota:        int(platformruntime.QuotaPerUnit * 3),
		ClaudeQuota:  int(platformruntime.QuotaPerUnit),
		UsedQuota:    int(platformruntime.QuotaPerUnit / 2),
		RequestCount: 12,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	plan := &commerceschema.SubscriptionPlan{
		Title:         "Standard 月卡",
		Enabled:       true,
		PlanType:      commerceschema.SubscriptionPlanTypeMonthly,
		TotalAmount:   int64(platformruntime.QuotaPerUnit * 100),
		DurationUnit:  commerceschema.SubscriptionDurationMonth,
		DurationValue: 1,
	}
	if err := db.Create(plan).Error; err != nil {
		t.Fatalf("failed to seed subscription plan: %v", err)
	}
	now := time.Now().Unix()
	if err := db.Create(&commerceschema.UserSubscription{
		UserId:      user.Id,
		PlanId:      plan.Id,
		Status:      "active",
		StartTime:   now - 3600,
		EndTime:     now + 30*24*3600,
		AmountTotal: int64(platformruntime.QuotaPerUnit * 100),
		AmountUsed:  int64(platformruntime.QuotaPerUnit * 24),
	}).Error; err != nil {
		t.Fatalf("failed to seed active subscription: %v", err)
	}

	token := seedDesktopToken(t, db, 1, identityapp.BuildDesktopTokenName("Default"), "desktoptoken123456")
	if err := auditapp.RecordLogTx(nil, user.Id, auditschema.LogTypeConsume, "desktop log entry"); err != nil {
		t.Fatalf("failed to seed log: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/account/summary", nil, 1)
	GetDesktopAccountSummary(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var payload desktopSummaryResponse
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode desktop summary: %v", err)
	}
	if payload.Account.Username != user.Username {
		t.Fatalf("expected username %q, got %q", user.Username, payload.Account.Username)
	}
	if payload.Account.QuotaUSD != 3 {
		t.Fatalf("expected quota usd 3, got %v", payload.Account.QuotaUSD)
	}
	if payload.Account.ClaudeQuotaUSD != 1 {
		t.Fatalf("expected claude quota usd 1, got %v", payload.Account.ClaudeQuotaUSD)
	}
	if payload.Account.UsedQuotaUSD != 0.5 {
		t.Fatalf("expected used quota usd 0.5, got %v", payload.Account.UsedQuotaUSD)
	}
	if len(payload.Subscriptions) != 1 {
		t.Fatalf("expected one active subscription, got %d", len(payload.Subscriptions))
	}
	if payload.Subscriptions[0].PlanTitle != "Standard 月卡" {
		t.Fatalf("expected active plan title, got %q", payload.Subscriptions[0].PlanTitle)
	}
	if payload.Subscriptions[0].AmountTotalUSD != 100 || payload.Subscriptions[0].AmountUsedUSD != 24 || payload.Subscriptions[0].RemainingUSD != 76 {
		t.Fatalf("unexpected subscription quota snapshot: %+v", payload.Subscriptions[0])
	}
	if payload.Subscriptions[0].Unlimited {
		t.Fatal("expected finite subscription quota")
	}
	if payload.Tokens.Total != 1 {
		t.Fatalf("expected token total 1, got %d", payload.Tokens.Total)
	}
	if payload.Tokens.DesktopToken == nil || payload.Tokens.DesktopToken.Key != token.GetMaskedKey() {
		t.Fatalf("expected masked desktop token %q", token.GetMaskedKey())
	}
}

func TestEnsureDesktopTokenCreatesAndReusesNamedDesktopToken(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{
		Id:          1,
		Username:    "desktop-owner",
		Password:    "password123",
		DisplayName: "Desktop Owner",
		Role:        constant.RoleCommonUser,
		Status:      constant.UserStatusEnabled,
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	body := map[string]any{"device_name": "Windows"}
	createCtx, createRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/tokens/ensure", body, 1)
	EnsureDesktopToken(createCtx)

	createResponse := decodeAPIResponse(t, createRecorder)
	if !createResponse.Success {
		t.Fatalf("expected create to succeed, got message: %s", createResponse.Message)
	}

	var created desktopEnsureTokenPayload
	if err := platformencoding.Unmarshal(createResponse.Data, &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	if !created.Created {
		t.Fatalf("expected created=true on first ensure")
	}
	expectedName := identityapp.BuildDesktopTokenName("Windows")
	if created.TokenName != expectedName {
		t.Fatalf("expected token name %q, got %q", expectedName, created.TokenName)
	}

	reuseCtx, reuseRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/tokens/ensure", body, 1)
	EnsureDesktopToken(reuseCtx)
	reuseResponse := decodeAPIResponse(t, reuseRecorder)
	if !reuseResponse.Success {
		t.Fatalf("expected reuse to succeed, got message: %s", reuseResponse.Message)
	}
}

func TestGetDesktopTokensReturnsWebsiteKeyList(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)

	user := &identityschema.User{Id: 1, Username: "desktop-token-list", Password: "password123", DisplayName: "Desktop Token List", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "website-key", "websitekey1234567890")
	seedDesktopToken(t, db, 2, "other-user-key", "otheruserkey123456")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/tokens?p=1&size=10", nil, 1)
	GetDesktopTokens(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var page struct {
		Total int                 `json:"total"`
		Items []tokenResponseItem `json:"items"`
	}
	if err := platformencoding.Unmarshal(response.Data, &page); err != nil {
		t.Fatalf("failed to decode desktop token page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected one desktop-visible website key, got total=%d items=%d", page.Total, len(page.Items))
	}
	if page.Items[0].Name != token.Name {
		t.Fatalf("expected website token %q, got %q", token.Name, page.Items[0].Name)
	}
}

func TestGetDesktopTokenKeyReturnsWebsiteKeyFullValue(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-key-owner", Password: "password123", DisplayName: "Desktop Key Owner", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "website-full-key", "websitefullkey1234567890")

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/tokens/"+strconv.Itoa(token.Id)+"/key", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetDesktopTokenKey(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
	var payload tokenKeyResponse
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode desktop token key response: %v", err)
	}
	if payload.Key != token.GetFullKey() {
		t.Fatalf("expected full website key %q, got %q", token.GetFullKey(), payload.Key)
	}
}

func TestGetDesktopGroupsReturnsUsableDropdownItems(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-groups", Password: "password123", DisplayName: "Desktop Groups", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	if err := db.Create(&gatewayschema.Ability{Group: "default", Model: "gpt-5.5", Enabled: true}).Error; err != nil {
		t.Fatalf("failed to seed ability: %v", err)
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/groups", nil, 1)
	GetDesktopGroups(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
	var payload identityapp.DesktopGroupsResponse
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode desktop groups: %v", err)
	}
	if payload.Current != "default" {
		t.Fatalf("expected current group default, got %q", payload.Current)
	}
}

func TestGetDesktopConfigTemplateReturnsToolSpecificEndpoints(t *testing.T) {
	platformconfig.OptionMapRWMutex.Lock()
	if platformconfig.OptionMap == nil {
		platformconfig.OptionMap = map[string]string{}
	}
	platformconfig.OptionMapRWMutex.Unlock()
	platformconfig.ServerAddress = "https://shu26.cfd"
	t.Cleanup(func() {
		platformconfig.ServerAddress = "http://localhost:3000"
	})

	tests := []struct {
		tool         string
		label        string
		authScheme   string
		modelFormat  string
		envKey       string
		endpointTail string
	}{
		{tool: "codex", label: "Codex", authScheme: "openai-api-key", modelFormat: "responses", envKey: "OPENAI_BASE_URL", endpointTail: "/v1"},
		{tool: "claude", label: "Claude Code", authScheme: "anthropic-auth-token", modelFormat: "anthropic", envKey: "ANTHROPIC_BASE_URL", endpointTail: ""},
	}

	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			target := "/api/desktop/config/template?tool=" + url.QueryEscape(tc.tool)
			ctx, recorder := newAuthenticatedContext(t, http.MethodGet, target, nil, 1)
			ctx.Request.URL.RawQuery = "tool=" + tc.tool
			GetDesktopConfigTemplate(ctx)
			response := decodeAPIResponse(t, recorder)
			if !response.Success {
				t.Fatalf("expected success response, got message: %s", response.Message)
			}
			var payload desktopTemplatePayload
			if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
				t.Fatalf("failed to decode template: %v", err)
			}
			if payload.Label != tc.label || payload.AuthScheme != tc.authScheme || payload.ModelFormat != tc.modelFormat {
				t.Fatalf("unexpected template payload: %#v", payload)
			}
		})
	}
}

func TestGetDesktopConfigTemplatesReturnsAllSupportedTools(t *testing.T) {
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/config/templates", nil, 1)
	GetDesktopConfigTemplates(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
}

func TestGetDesktopConfigTemplateRejectsUnsupportedTool(t *testing.T) {
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/config/template?tool=cursor", nil, 1)
	ctx.Request.URL.RawQuery = "tool=cursor"
	GetDesktopConfigTemplate(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected unsupported tool template request to fail")
	}
}

func TestGetDesktopServiceStatusReturnsSummary(t *testing.T) {
	originalNotice := platformconfig.OptionMap["Notice"]
	originalMaintenance := platformconfig.OptionMap["Maintenance"]
	platformconfig.OptionMap["Notice"] = "Desktop maintenance notice"
	platformconfig.OptionMap["Maintenance"] = "true"
	t.Cleanup(func() {
		platformconfig.OptionMap["Notice"] = originalNotice
		platformconfig.OptionMap["Maintenance"] = originalMaintenance
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/service/status", nil, 1)
	GetDesktopServiceStatus(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
}

func TestDesktopServiceMaintenanceEnabledParsesCommonTruthies(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "true", raw: "true", want: true},
		{name: "false", raw: "false", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := identityapp.DesktopServiceMaintenanceEnabled(map[string]string{"Maintenance": tc.raw})
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestGetDesktopTokenConfigReturnsPerToolPayloads(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-config-user", Password: "password123", DisplayName: "Desktop Config User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "desktop-config-token", "desktopconfigtoken123456")
	if err := db.Create(&gatewayschema.Ability{Group: "default", Model: "gpt-5.5", Enabled: true}).Error; err != nil {
		t.Fatalf("failed to seed ability: %v", err)
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/tokens/"+strconv.Itoa(token.Id)+"/config", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetDesktopTokenConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
	if !strings.Contains(string(response.Data), `"icon":"codego"`) {
		t.Fatalf("expected CodeGo icon in token config response, got: %s", response.Data)
	}
}

func TestGetDesktopTokenConfigUsesTokenGroupModels(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-token-group", Password: "password123", DisplayName: "Desktop Token Group", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "group-token", "grouptoken123456")
	token.Group = "default"
	if err := db.Save(token).Error; err != nil {
		t.Fatalf("failed to update token group: %v", err)
	}
	if err := db.Create(&gatewayschema.Ability{Group: "default", Model: "gpt-5.5", Enabled: true}).Error; err != nil {
		t.Fatalf("failed to seed ability: %v", err)
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/tokens/"+strconv.Itoa(token.Id)+"/config", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetDesktopTokenConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
}

func TestGetDesktopTokenConfigRejectsInvalidTokenID(t *testing.T) {
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/tokens/abc/config", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: "abc"}}
	GetDesktopTokenConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected invalid token id request to fail")
	}
}

func TestGetDesktopTokenConfigRejectsOtherUsersToken(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	users := []*identityschema.User{
		{Id: 1, Username: "owner-a", Password: "password123", DisplayName: "Owner A", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "affa"},
		{Id: 2, Username: "owner-b", Password: "password123", DisplayName: "Owner B", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "affb"},
	}
	for _, user := range users {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("failed to seed user %d: %v", user.Id, err)
		}
	}
	token := seedDesktopToken(t, db, 1, "owned-token", "privatekey123456")
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/tokens/"+strconv.Itoa(token.Id)+"/config", nil, 2)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetDesktopTokenConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected unauthorized token access to fail")
	}
}

func TestGetDesktopUsageLogsPassesThroughUserLogsPage(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-usage-logs", Password: "password123", DisplayName: "Desktop Usage Logs", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	if err := auditapp.RecordLogTx(nil, user.Id, auditschema.LogTypeConsume, "usage log 1"); err != nil {
		t.Fatalf("failed to seed log: %v", err)
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/usage/logs?p=1&size=10", nil, 1)
	GetDesktopUsageLogs(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
}

func TestGetDesktopUsageTrendsReturnsFilledDailySeries(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{Id: 1, Username: "desktop-usage-trends", Password: "password123", DisplayName: "Desktop Usage Trends", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	rows := []auditdomain.QuotaData{
		{UserID: user.Id, Username: user.Username, CreatedAt: yesterday.Unix(), Count: 2, Quota: int(platformruntime.QuotaPerUnit), TokenUsed: 300},
		{UserID: user.Id, Username: user.Username, CreatedAt: now.Unix(), Count: 1, Quota: int(platformruntime.QuotaPerUnit / 2), TokenUsed: 120},
	}
	for _, row := range rows {
		item := row
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("failed to seed quota data: %v", err)
		}
	}
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/usage/trends?days=7", nil, 1)
	ctx.Request.URL.RawQuery = "days=7"
	GetDesktopUsageTrends(ctx)
	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}
}

func TestCreateDesktopImportConfigAndConsumeCodeOnce(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	user := &identityschema.User{Id: 1, Username: "desktop-import-user", Password: "password123", DisplayName: "Desktop Import User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "codex-key", "importkey1234567890")
	platformconfig.ServerAddress = "https://shu26.cfd"
	t.Cleanup(func() {
		platformconfig.ServerAddress = "http://localhost:3000"
		resetDesktopImportCacheForTest(t)
	})
	body := map[string]any{"tool": "codex", "token_id": token.Id, "name": "Code Go Codex Import", "model": "gpt-5.5"}
	createCtx, createRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 1)
	CreateDesktopImportConfig(createCtx)
	createResponse := decodeAPIResponse(t, createRecorder)
	if !createResponse.Success {
		t.Fatalf("expected create import config success, got %s", createResponse.Message)
	}
	var created desktopImportCreatePayload
	if err := platformencoding.Unmarshal(createResponse.Data, &created); err != nil {
		t.Fatalf("failed to decode create import response: %v", err)
	}
	codeQuery := "code=" + url.QueryEscape(created.Code)
	getCtx, getRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/import/config?"+codeQuery, nil, 0)
	getCtx.Request.URL.RawQuery = codeQuery
	GetDesktopImportConfig(getCtx)
	getResponse := decodeAPIResponse(t, getRecorder)
	if !getResponse.Success {
		t.Fatalf("expected import config fetch success, got %s", getResponse.Message)
	}
}

func TestCreateCCSwitchImportUsesOfficialDirectParameterContract(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	user := &identityschema.User{Id: 1, Username: "ccswitch-import-user", Password: "password123", DisplayName: "CC Switch Import User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "ccswitch-key", "ccswitchsecret123456")
	platformconfig.ServerAddress = "https://shu26.cfd"
	t.Cleanup(func() {
		platformconfig.ServerAddress = "http://localhost:3000"
		resetDesktopImportCacheForTest(t)
	})

	body := map[string]any{"target": "ccswitch", "tool": "codex", "token_id": token.Id, "name": "CodeGo", "model": "gpt-5.6-luna"}
	createCtx, createRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 1)
	CreateDesktopImportConfig(createCtx)
	createResponse := decodeAPIResponse(t, createRecorder)
	if !createResponse.Success {
		t.Fatalf("expected CC Switch import success, got %s", createResponse.Message)
	}
	if strings.Contains(string(createResponse.Data), `"config":`) {
		t.Fatalf("create response must not expose the encoded API key config")
	}

	var created desktopImportCreatePayload
	if err := platformencoding.Unmarshal(createResponse.Data, &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	deepLink, err := url.Parse(created.DeepLink)
	if err != nil {
		t.Fatalf("failed to parse deep link: %v", err)
	}
	if deepLink.Scheme != "ccswitch" {
		t.Fatalf("expected ccswitch scheme, got %q", deepLink.Scheme)
	}
	params := deepLink.Query()
	for _, key := range []string{"config", "configFormat", "configUrl", "codegoAction", "tokenId"} {
		if params.Has(key) {
			t.Fatalf("CC Switch link must not contain %s", key)
		}
	}
	if params.Get("apiKey") != token.GetFullKey() {
		t.Fatalf("expected direct API key parameter")
	}
	if params.Get("usageEnabled") != "true" || params.Get("usageAutoInterval") != "10" {
		t.Fatalf("expected enabled CC Switch balance query configuration")
	}
	usageScript, err := base64.StdEncoding.DecodeString(params.Get("usageScript"))
	if err != nil {
		t.Fatalf("failed to decode CC Switch balance usage script: %v", err)
	}
	if !strings.Contains(string(usageScript), "{{baseUrl}}/dashboard/balance") || !strings.Contains(string(usageScript), "{{apiKey}}") {
		t.Fatalf("unexpected CC Switch balance usage script: %s", usageScript)
	}
	if params.Get("endpoint") != "https://shu26.cfd/v1" {
		t.Fatalf("unexpected endpoint %q", params.Get("endpoint"))
	}
	if params.Get("model") != "gpt-5.6-luna" {
		t.Fatalf("unexpected model %q", params.Get("model"))
	}
	if params.Get("homepage") != "https://shu26.cfd" || params.Get("enabled") != "true" {
		t.Fatalf("missing official provider metadata in %q", created.DeepLink)
	}
	if params.Get("notes") != "Imported from CodeGo website" {
		t.Fatalf("unexpected CC Switch import notes %q", params.Get("notes"))
	}
	if created.Code != "" || created.ConfigURL != "" || created.ExpiresIn != 0 {
		t.Fatalf("CC Switch direct import must not create a one-time config: %+v", created)
	}
}

func TestGetTokenAccountBalanceAppliesTokenLimit(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	user := &identityschema.User{
		Id:       1,
		Username: "balance-user",
		Password: "password123",
		Role:     constant.RoleCommonUser,
		Status:   constant.UserStatusEnabled,
		Group:    "default",
		Quota:    int(platformruntime.QuotaPerUnit * 3),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := &identityschema.Token{
		UserId:         user.Id,
		Name:           "limited-balance-key",
		Key:            "balancekey1234567890",
		Status:         constant.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    int(platformruntime.QuotaPerUnit * 2),
		UnlimitedQuota: false,
		Group:          "default",
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to seed token: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/v1/dashboard/balance", nil, user.Id)
	ctx.Set("token_id", token.Id)
	GetTokenAccountBalance(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected balance query success, got %s", response.Message)
	}
	var payload struct {
		AvailableUSD        float64  `json:"available_usd"`
		AccountAvailableUSD float64  `json:"account_available_usd"`
		WalletAvailableUSD  float64  `json:"wallet_available_usd"`
		TokenAvailableUSD   *float64 `json:"token_available_usd"`
		TokenUnlimited      bool     `json:"token_unlimited"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode balance payload: %v", err)
	}
	if payload.AccountAvailableUSD != 3 || payload.WalletAvailableUSD != 3 || payload.AvailableUSD != 2 {
		t.Fatalf("unexpected balance payload: %+v", payload)
	}
	if payload.TokenAvailableUSD == nil || *payload.TokenAvailableUSD != 2 || payload.TokenUnlimited {
		t.Fatalf("expected finite token limit in payload: %+v", payload)
	}
}

func TestCreateDesktopImportConfigSupportsAllDesktopTools(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	user := &identityschema.User{Id: 1, Username: "desktop-import-matrix", Password: "password123", DisplayName: "Desktop Import Matrix", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "matrix-token", "matriximport1234567890")
	platformconfig.ServerAddress = "https://shu26.cfd"
	t.Cleanup(func() {
		platformconfig.ServerAddress = "http://localhost:3000"
		resetDesktopImportCacheForTest(t)
	})
	tests := []struct {
		tool               string
		model              string
		wantConfigContains string
	}{
		{tool: "claude", model: "claude-sonnet-4-5", wantConfigContains: "ANTHROPIC_AUTH_TOKEN"},
		{tool: "codex", model: "gpt-5.5", wantConfigContains: "\"auth\""},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			body := map[string]any{"tool": tc.tool, "token_id": token.Id, "name": "Code Go " + tc.tool, "model": tc.model}
			createCtx, createRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 1)
			CreateDesktopImportConfig(createCtx)
			createResponse := decodeAPIResponse(t, createRecorder)
			if !createResponse.Success {
				t.Fatalf("expected create import config success, got %s", createResponse.Message)
			}
			var created desktopImportCreatePayload
			if err := platformencoding.Unmarshal(createResponse.Data, &created); err != nil {
				t.Fatalf("failed to decode create import response: %v", err)
			}
			codeQuery := "code=" + url.QueryEscape(created.Code)
			getCtx, getRecorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/import/config?"+codeQuery, nil, 0)
			getCtx.Request.URL.RawQuery = codeQuery
			GetDesktopImportConfig(getCtx)
			getResponse := decodeAPIResponse(t, getRecorder)
			if !getResponse.Success {
				t.Fatalf("expected import config fetch success, got %s", getResponse.Message)
			}
			var payload desktopImportConfigResponse
			if err := platformencoding.Unmarshal(getResponse.Data, &payload); err != nil {
				t.Fatalf("failed to decode import config payload: %v", err)
			}
			rawConfig, err := base64.StdEncoding.DecodeString(payload.Config)
			if err != nil {
				t.Fatalf("failed to decode base64 config for %s: %v", tc.tool, err)
			}
			if !strings.Contains(string(rawConfig), tc.wantConfigContains) {
				t.Fatalf("expected decoded config for %s to contain %q, got %q", tc.tool, tc.wantConfigContains, string(rawConfig))
			}
		})
	}
}

func TestGetDesktopImportConfigRequiresCode(t *testing.T) {
	resetDesktopImportCacheForTest(t)
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/desktop/import/config", nil, 0)
	GetDesktopImportConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected missing code request to fail")
	}
}

func TestCreateDesktopImportConfigRejectsOtherUsersToken(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	users := []*identityschema.User{
		{Id: 1, Username: "owner-a", Password: "password123", DisplayName: "Owner A", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "affa"},
		{Id: 2, Username: "owner-b", Password: "password123", DisplayName: "Owner B", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: "affb"},
	}
	for _, user := range users {
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("failed to seed user %d: %v", user.Id, err)
		}
	}
	token := seedDesktopToken(t, db, 1, "owned-token", "privatekey123456")
	body := map[string]any{"tool": "claude", "token_id": token.Id, "name": "Code Go Claude", "model": "claude-sonnet-4-5"}
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 2)
	CreateDesktopImportConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected unauthorized token access to fail")
	}
}

func TestCreateDesktopImportConfigRejectsUnsupportedTool(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	user := &identityschema.User{Id: 1, Username: "unsupported-tool-user", Password: "password123", DisplayName: "Unsupported Tool User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "supported-token", "supportedtoken123456")
	body := map[string]any{"tool": "cursor", "token_id": token.Id, "name": "Code Go Cursor"}
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 1)
	CreateDesktopImportConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected unsupported tool import config creation to fail")
	}
}

func TestCreateDesktopImportConfigRejectsUnsupportedTarget(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	user := &identityschema.User{Id: 1, Username: "unsupported-target-user", Password: "password123", DisplayName: "Unsupported Target User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "target-token", "targettoken123456")
	body := map[string]any{"target": "other", "tool": "codex", "token_id": token.Id, "name": "Code Go Codex"}
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 1)
	CreateDesktopImportConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success || response.Message != "unsupported desktop target" {
		t.Fatalf("expected unsupported desktop target error, got %+v", response)
	}
}

func TestCreateDesktopImportConfigRejectsDisabledToken(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	resetDesktopImportCacheForTest(t)
	user := &identityschema.User{Id: 1, Username: "disabled-token-user", Password: "password123", DisplayName: "Disabled Token User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	token := seedDesktopToken(t, db, 1, "disabled-import-token", "disabledimport123456")
	if err := db.Model(&identityschema.Token{}).Where("id = ?", token.Id).Update("status", constant.TokenStatusDisabled).Error; err != nil {
		t.Fatalf("failed to disable token: %v", err)
	}
	body := map[string]any{"tool": "gemini", "token_id": token.Id, "name": "Code Go Gemini", "model": "gemini-2.5-pro"}
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/desktop/import/deeplink", body, 1)
	CreateDesktopImportConfig(ctx)
	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected disabled token import config creation to fail")
	}
}
