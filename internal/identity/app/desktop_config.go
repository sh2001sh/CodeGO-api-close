package app

import (
	"errors"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformtext "github.com/sh2001sh/new-api/internal/platform/textx"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type DesktopConfigTemplate struct {
	Tool            string            `json:"tool"`
	Label           string            `json:"label"`
	ServerAddress   string            `json:"server_address"`
	Endpoint        string            `json:"endpoint"`
	AuthScheme      string            `json:"auth_scheme"`
	ModelFormat     string            `json:"model_format"`
	Env             map[string]string `json:"env"`
	DefaultProvider string            `json:"default_provider"`
}

type DesktopConfigTemplatesResponse struct {
	BaseURL string                           `json:"base_url"`
	Tools   map[string]DesktopConfigTemplate `json:"tools"`
}

type DesktopTokenConfigResponse struct {
	Token         *identityschema.Token                 `json:"token"`
	ServerAddress string                                `json:"server_address"`
	Tools         map[string]DesktopImportConfigPayload `json:"tools"`
}

type DesktopImportConfigPayload struct {
	Tool         string `json:"tool"`
	Name         string `json:"name"`
	Homepage     string `json:"homepage"`
	Endpoint     string `json:"endpoint"`
	APIKey       string `json:"apiKey"`
	Model        string `json:"model,omitempty"`
	HaikuModel   string `json:"haikuModel,omitempty"`
	SonnetModel  string `json:"sonnetModel,omitempty"`
	OpusModel    string `json:"opusModel,omitempty"`
	Enabled      bool   `json:"enabled"`
	Config       string `json:"config"`
	ConfigFormat string `json:"configFormat"`
	Icon         string `json:"icon,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

type DesktopImportCreateRequest struct {
	Target      string `json:"target"`
	Tool        string `json:"tool"`
	TokenID     int    `json:"token_id"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	HaikuModel  string `json:"haiku_model"`
	SonnetModel string `json:"sonnet_model"`
	OpusModel   string `json:"opus_model"`
	Enabled     *bool  `json:"enabled"`
}

type DesktopImportCreateResponse struct {
	Code      string `json:"code"`
	DeepLink  string `json:"deep_link"`
	ConfigURL string `json:"config_url"`
	ExpiresIn int64  `json:"expires_in_seconds"`
	Tool      string `json:"tool"`
	TokenName string `json:"token_name"`
	Provider  string `json:"provider_name"`
}

// BuildDesktopConfigTemplate returns the config template for a supported desktop tool.
func BuildDesktopConfigTemplate(tool string) (*DesktopConfigTemplate, error) {
	tool = NormalizeDesktopTool(tool)
	if tool == "" {
		return nil, errors.New("unsupported tool")
	}

	serverAddress := NormalizeDesktopServerAddress("")
	template := &DesktopConfigTemplate{
		Tool:            tool,
		ServerAddress:   serverAddress,
		DefaultProvider: "codego",
	}

	switch tool {
	case DesktopToolCodex:
		template.Label = "Codex"
		template.Endpoint = serverAddress + "/v1"
		template.AuthScheme = "openai-api-key"
		template.ModelFormat = "responses"
		template.Env = map[string]string{
			"OPENAI_BASE_URL": serverAddress + "/v1",
			"OPENAI_API_BASE": serverAddress + "/v1",
		}
	case DesktopToolClaude:
		template.Label = "Claude Code"
		template.Endpoint = serverAddress
		template.AuthScheme = "anthropic-auth-token"
		template.ModelFormat = "anthropic"
		template.Env = map[string]string{
			"ANTHROPIC_BASE_URL": serverAddress,
		}
	case DesktopToolGemini:
		template.Label = "Gemini CLI"
		template.Endpoint = serverAddress
		template.AuthScheme = "google-api-key-compatible"
		template.ModelFormat = "gemini"
		template.Env = map[string]string{
			"GOOGLE_API_BASE": serverAddress,
		}
	case DesktopToolOpenCode:
		template.Label = "OpenCode"
		template.Endpoint = serverAddress + "/v1"
		template.AuthScheme = "openai-compatible-api-key"
		template.ModelFormat = "openai-compatible"
		template.Env = map[string]string{
			"OPENAI_BASE_URL": serverAddress + "/v1",
		}
	case DesktopToolOpenClaw:
		template.Label = "OpenClaw"
		template.Endpoint = serverAddress + "/v1"
		template.AuthScheme = "openai-compatible-api-key"
		template.ModelFormat = "openai-compatible"
		template.Env = map[string]string{
			"OPENAI_BASE_URL": serverAddress + "/v1",
		}
	case DesktopToolHermes:
		template.Label = "Hermes"
		template.Endpoint = serverAddress + "/v1"
		template.AuthScheme = "openai-compatible-api-key"
		template.ModelFormat = "chat-completions"
		template.Env = map[string]string{
			"OPENAI_BASE_URL": serverAddress + "/v1",
		}
	}

	return template, nil
}

// BuildDesktopConfigTemplatesResponse returns all supported desktop config templates.
func BuildDesktopConfigTemplatesResponse() (*DesktopConfigTemplatesResponse, error) {
	baseURL := NormalizeDesktopServerAddress("") + "/v1"
	tools := make(map[string]DesktopConfigTemplate)
	for _, tool := range []string{
		DesktopToolCodex,
		DesktopToolClaude,
		DesktopToolGemini,
		DesktopToolOpenCode,
		DesktopToolOpenClaw,
		DesktopToolHermes,
	} {
		template, err := BuildDesktopConfigTemplate(tool)
		if err != nil {
			return nil, err
		}
		tools[tool] = *template
	}
	return &DesktopConfigTemplatesResponse{
		BaseURL: baseURL,
		Tools:   tools,
	}, nil
}

// BuildDesktopTokenConfigResponse builds per-tool config payloads for a desktop token.
func BuildDesktopTokenConfigResponse(token *identityschema.Token, availableModels []string) (*DesktopTokenConfigResponse, error) {
	toolRequests := map[string]DesktopImportCreateRequest{
		DesktopToolCodex: {
			Tool:  DesktopToolCodex,
			Name:  desktopProviderDisplayName(DesktopToolCodex),
			Model: pickDesktopRecommendedModel(availableModels, "gpt-5.5", "gpt-5", "o3", "o4"),
		},
		DesktopToolClaude: {
			Tool:        DesktopToolClaude,
			Name:        desktopProviderDisplayName(DesktopToolClaude),
			Model:       pickDesktopRecommendedModel(availableModels, "claude-sonnet-4-5", "claude-sonnet", "claude-3-7-sonnet"),
			HaikuModel:  pickDesktopRecommendedModel(availableModels, "claude-3-5-haiku", "claude-haiku"),
			SonnetModel: pickDesktopRecommendedModel(availableModels, "claude-sonnet-4-5", "claude-sonnet"),
			OpusModel:   pickDesktopRecommendedModel(availableModels, "claude-opus-4", "claude-opus"),
		},
		DesktopToolGemini: {
			Tool:  DesktopToolGemini,
			Name:  desktopProviderDisplayName(DesktopToolGemini),
			Model: pickDesktopRecommendedModel(availableModels, "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0"),
		},
		DesktopToolOpenCode: {
			Tool:  DesktopToolOpenCode,
			Name:  desktopProviderDisplayName(DesktopToolOpenCode),
			Model: pickDesktopRecommendedModel(availableModels, "gpt-5.5", "gpt-5", "o3", "o4"),
		},
		DesktopToolOpenClaw: {
			Tool:  DesktopToolOpenClaw,
			Name:  desktopProviderDisplayName(DesktopToolOpenClaw),
			Model: pickDesktopRecommendedModel(availableModels, "gpt-5.5", "gpt-5", "o3", "o4"),
		},
		DesktopToolHermes: {
			Tool:  DesktopToolHermes,
			Name:  desktopProviderDisplayName(DesktopToolHermes),
			Model: pickDesktopRecommendedModel(availableModels, "gpt-5.5", "gpt-5", "o3", "o4"),
		},
	}

	tools := make(map[string]DesktopImportConfigPayload, len(toolRequests))
	for tool, req := range toolRequests {
		payload, err := BuildDesktopImportConfig(tool, token, req)
		if err != nil {
			return nil, err
		}
		tools[tool] = *payload
	}

	return &DesktopTokenConfigResponse{
		Token:         BuildMaskedTokenResponse(token),
		ServerAddress: NormalizeDesktopServerAddress(""),
		Tools:         tools,
	}, nil
}

// BuildDesktopImportConfig builds the one-shot import payload for a desktop tool.
func BuildDesktopImportConfig(tool string, token *identityschema.Token, req DesktopImportCreateRequest) (*DesktopImportConfigPayload, error) {
	template, err := BuildDesktopConfigTemplate(tool)
	if err != nil {
		return nil, err
	}

	payload := &DesktopImportConfigPayload{
		Tool:         tool,
		Name:         strings.TrimSpace(req.Name),
		Homepage:     template.ServerAddress,
		Endpoint:     template.Endpoint,
		APIKey:       token.GetFullKey(),
		Model:        strings.TrimSpace(req.Model),
		HaikuModel:   strings.TrimSpace(req.HaikuModel),
		SonnetModel:  strings.TrimSpace(req.SonnetModel),
		OpusModel:    strings.TrimSpace(req.OpusModel),
		Enabled:      req.Enabled == nil || *req.Enabled,
		ConfigFormat: "json",
		Icon:         desktopToolIcon(tool),
		Notes:        "Generated by Code Go Desktop import",
	}
	if payload.Name == "" {
		payload.Name = desktopProviderDisplayName(tool)
	}

	switch tool {
	case DesktopToolClaude:
		env := map[string]string{
			"ANTHROPIC_BASE_URL":   template.Endpoint,
			"ANTHROPIC_AUTH_TOKEN": payload.APIKey,
		}
		if payload.Model != "" {
			env["ANTHROPIC_MODEL"] = payload.Model
		}
		if payload.HaikuModel != "" {
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = payload.HaikuModel
		}
		if payload.SonnetModel != "" {
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = payload.SonnetModel
		}
		if payload.OpusModel != "" {
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = payload.OpusModel
		}
		configBody, err := platformencoding.Marshal(map[string]any{"env": env})
		if err != nil {
			return nil, err
		}
		payload.Config = platformtext.EncodeBase64(string(configBody))
	case DesktopToolGemini:
		env := map[string]string{
			"GEMINI_API_KEY":         payload.APIKey,
			"GOOGLE_GEMINI_BASE_URL": template.Endpoint,
		}
		if payload.Model != "" {
			env["GEMINI_MODEL"] = payload.Model
		}
		configBody, err := platformencoding.Marshal(env)
		if err != nil {
			return nil, err
		}
		payload.Config = platformtext.EncodeBase64(string(configBody))
	case DesktopToolCodex:
		modelName := payload.Model
		if modelName == "" {
			modelName = "gpt-5.5"
		}
		configText := "model_provider = \"custom\"\n" +
			"model = " + strconvQuote(modelName) + "\n" +
			"model_reasoning_effort = \"high\"\n" +
			"disable_response_storage = true\n\n" +
			"[model_providers.custom]\n" +
			"name = \"Code Go\"\n" +
			"base_url = " + strconvQuote(template.Endpoint) + "\n" +
			"wire_api = \"responses\"\n" +
			"requires_openai_auth = true\n"
		configBody, err := platformencoding.Marshal(map[string]any{
			"auth": map[string]string{
				"OPENAI_API_KEY": payload.APIKey,
			},
			"config": configText,
		})
		if err != nil {
			return nil, err
		}
		payload.Config = platformtext.EncodeBase64(string(configBody))
	case DesktopToolOpenCode:
		modelName := payload.Model
		if modelName == "" {
			modelName = "gpt-5.5"
		}
		configBody, err := platformencoding.Marshal(map[string]any{
			"npm":  "@ai-sdk/openai-compatible",
			"name": payload.Name,
			"options": map[string]any{
				"baseURL":     template.Endpoint,
				"apiKey":      payload.APIKey,
				"setCacheKey": true,
			},
			"models": map[string]any{
				modelName: map[string]any{
					"name": modelName,
				},
			},
		})
		if err != nil {
			return nil, err
		}
		payload.Config = platformtext.EncodeBase64(string(configBody))
	case DesktopToolOpenClaw:
		modelName := payload.Model
		if modelName == "" {
			modelName = "gpt-5.5"
		}
		configBody, err := platformencoding.Marshal(map[string]any{
			"baseUrl": template.Endpoint,
			"apiKey":  payload.APIKey,
			"api":     "openai-completions",
			"models": []map[string]any{
				{
					"id":   modelName,
					"name": modelName,
				},
			},
		})
		if err != nil {
			return nil, err
		}
		payload.Config = platformtext.EncodeBase64(string(configBody))
	case DesktopToolHermes:
		modelName := payload.Model
		if modelName == "" {
			modelName = "gpt-5.5"
		}
		configBody, err := platformencoding.Marshal(map[string]any{
			"name":     payload.Name,
			"base_url": template.Endpoint,
			"api_key":  payload.APIKey,
			"api_mode": "chat_completions",
			"models": []map[string]any{
				{
					"id":   modelName,
					"name": modelName,
				},
			},
		})
		if err != nil {
			return nil, err
		}
		payload.Config = platformtext.EncodeBase64(string(configBody))
	default:
		return nil, errors.New("unsupported tool")
	}

	return payload, nil
}

// BuildDesktopImportConfigDeepLink creates a target-specific desktop import link.
func BuildDesktopImportConfigDeepLink(userID int, req DesktopImportCreateRequest) (*DesktopImportCreateResponse, error) {
	target := strings.ToLower(strings.TrimSpace(req.Target))
	if target == "" {
		target = "codego"
	}
	if target != "codego" && target != "ccswitch" {
		return nil, errors.New("unsupported desktop target")
	}
	tool := NormalizeDesktopTool(req.Tool)
	if tool == "" {
		return nil, errors.New("unsupported tool")
	}
	if req.TokenID <= 0 {
		return nil, errors.New("invalid token_id")
	}

	token, err := FindDesktopTokenByID(userID, req.TokenID)
	if err != nil {
		return nil, err
	}
	if token.Status != constant.TokenStatusEnabled {
		return nil, errors.New("token is not enabled")
	}

	payload, err := BuildDesktopImportConfig(tool, token, req)
	if err != nil {
		return nil, err
	}
	if target == "ccswitch" {
		payload.Notes = "Imported from CodeGo website"
	}

	serverAddress := NormalizeDesktopServerAddress("")
	params := url.Values{}
	params.Set("resource", "provider")
	params.Set("app", tool)
	params.Set("name", payload.Name)
	params.Set("endpoint", payload.Endpoint)
	params.Set("homepage", payload.Homepage)
	params.Set("enabled", strconv.FormatBool(payload.Enabled))
	params.Set("icon", payload.Icon)
	code := ""
	configURL := ""
	expiresIn := int64(0)
	if target == "codego" {
		code = platformruntime.GetRandomString(32)
		if code == "" {
			return nil, errors.New("failed to generate import code")
		}
		if err = getDesktopImportCache().SetWithTTL(code, *payload, desktopImportCodeTTL); err != nil {
			return nil, err
		}
		configURL = serverAddress + "/api/desktop/import/config?code=" + url.QueryEscape(code)
		expiresIn = int64(desktopImportCodeTTL / time.Second)
		params.Set("codegoAction", "applyToolConfig")
		params.Set("configUrl", configURL)
		params.Set("configFormat", payload.ConfigFormat)
		params.Set("tokenId", strconv.Itoa(req.TokenID))
	} else {
		params.Set("apiKey", payload.APIKey)
		params.Set("usageEnabled", "true")
		params.Set("usageScript", platformtext.EncodeBase64(ccSwitchBalanceUsageScript))
		params.Set("usageAutoInterval", "10")
	}
	if payload.Model != "" {
		params.Set("model", payload.Model)
	}
	if payload.HaikuModel != "" {
		params.Set("haikuModel", payload.HaikuModel)
	}
	if payload.SonnetModel != "" {
		params.Set("sonnetModel", payload.SonnetModel)
	}
	if payload.OpusModel != "" {
		params.Set("opusModel", payload.OpusModel)
	}
	if payload.Notes != "" {
		params.Set("notes", payload.Notes)
	}

	protocol := "codego"
	if target == "ccswitch" {
		protocol = "ccswitch"
	}
	return &DesktopImportCreateResponse{
		Code:      code,
		DeepLink:  protocol + "://v1/import?" + params.Encode(),
		ConfigURL: configURL,
		ExpiresIn: expiresIn,
		Tool:      tool,
		TokenName: token.Name,
		Provider:  payload.Name,
	}, nil
}

const ccSwitchBalanceUsageScript = `({
  request: {
    url: "{{baseUrl}}/dashboard/balance",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}"
    }
  },
  extractor: function(response) {
    if (!response.success || !response.data) {
      return {
        isValid: false,
        invalidMessage: response.message || "Balance query failed"
      };
    }
    var balance = response.data;
    var subscriptionText = (balance.subscriptions || []).map(function(item) {
      return item.name + ": $" + Number(item.remaining_usd || 0).toFixed(2);
    }).join(" | ");
    return {
      isValid: true,
      planName: balance.subscription_available_usd > 0 ? "Code Go wallet + plans" : "Code Go wallet",
      remaining: Number(balance.available_usd || 0),
      total: Number(balance.account_available_usd || 0),
      used: 0,
      unit: balance.currency || "USD",
      extra: subscriptionText
    };
  }
})`

// ResolveDesktopImportConfig resolves and consumes a one-time desktop import code.
func ResolveDesktopImportConfig(code string) (*DesktopImportConfigPayload, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("missing code")
	}

	cache := getDesktopImportCache()
	payload, found, err := cache.Get(code)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("import code is invalid or expired")
	}
	if _, err = cache.DeleteMany([]string{code}); err != nil {
		return nil, err
	}
	return &payload, nil
}
