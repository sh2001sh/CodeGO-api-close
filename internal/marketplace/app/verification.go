package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
)

func probeMarketplaceChannel(
	ctx context.Context,
	channel *marketplaceschema.Channel,
	reportStage func(string),
	reportResults func([]ModelVerificationResult),
) ([]ModelVerificationResult, error) {
	return probeMarketplaceChannelModels(ctx, channel, nil, reportStage, reportResults)
}

func probeMarketplaceChannelModels(
	ctx context.Context,
	channel *marketplaceschema.Channel,
	targetModels []string,
	reportStage func(string),
	reportResults func([]ModelVerificationResult),
) ([]ModelVerificationResult, error) {
	reportStage("basic_security")
	baseURL, err := platformsecurity.DecryptSecret(channel.BaseURLCiphertext)
	if err != nil {
		return nil, err
	}
	if err := ValidateMarketplaceURL(baseURL); err != nil {
		return nil, err
	}
	credential, err := platformsecurity.DecryptSecret(channel.CredentialCiphertext)
	if err != nil {
		return nil, err
	}
	reportStage("model_list")
	models, err := fetchUpstreamModelsContext(ctx, channel.ProviderType, baseURL, credential)
	if err != nil {
		return nil, err
	}
	declared := normalizeModels(targetModels)
	if len(declared) == 0 {
		declared = decodeModels(channel.DeclaredModels)
	}
	reportStage("model_match")
	modelListErr := verifyDeclaredModels(declared, models)
	reportStage("inference")
	results, inferenceErr := probeDeclaredModels(
		ctx, channel.ProviderType, baseURL, credential, declared, models, reportResults,
	)
	return results, errors.Join(modelListErr, inferenceErr)
}

func FetchUpstreamModels(req FetchModelsRequest) ([]string, error) {
	if err := validateProvider(req.ProviderType); err != nil {
		return nil, err
	}
	if err := ValidateMarketplaceURL(req.BaseURL); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return nil, errors.New("API Key 不能为空")
	}
	return fetchUpstreamModels(req.ProviderType, req.BaseURL, req.APIKey)
}

func fetchUpstreamModels(provider, baseURL, apiKey string) ([]string, error) {
	return fetchUpstreamModelsContext(context.Background(), provider, baseURL, apiKey)
}

func fetchUpstreamModelsContext(
	parent context.Context,
	provider, baseURL, apiKey string,
) ([]string, error) {
	endpoint, err := modelsEndpoint(provider, baseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setMarketplaceAuthHeaders(httpReq, provider, apiKey)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		message := strings.TrimSpace(string(detail))
		if message == "" {
			message = response.Status
		}
		return nil, fmt.Errorf("获取模型列表失败（HTTP %d）: %s", response.StatusCode, message)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("上游模型列表响应格式无效")
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		models = append(models, item.ID)
	}
	for _, item := range payload.Models {
		models = append(models, strings.TrimPrefix(item.Name, "models/"))
	}
	models = normalizeModels(models)
	if len(models) == 0 {
		return nil, errors.New("上游未返回可用模型")
	}
	return models, nil
}

func setMarketplaceAuthHeaders(req *http.Request, provider, apiKey string) {
	apiKey = strings.TrimSpace(apiKey)
	if provider == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		return
	}
	if provider == "azure_openai" {
		req.Header.Set("api-key", apiKey)
		return
	}
	if provider == "gemini" {
		req.Header.Set("x-goog-api-key", apiKey)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func modelsEndpoint(provider, baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	if provider == "gemini" && !strings.HasSuffix(parsed.Path, "/v1beta") {
		parsed.Path += "/v1beta/models"
	} else if strings.HasSuffix(parsed.Path, "/v1") || strings.HasSuffix(parsed.Path, "/v1beta") {
		parsed.Path += "/models"
	} else {
		parsed.Path += "/v1/models"
	}
	return parsed.String(), nil
}

func completeVerification(run *marketplaceschema.VerificationRun, channel *marketplaceschema.Channel, group *marketplaceschema.Group, results []ModelVerificationResult, probeErr error) {
	now := time.Now().UTC()
	expires := now.Add(7 * 24 * time.Hour)
	status := marketplacedomain.VerificationPassed
	lifecycle := marketplacedomain.LifecycleDraft
	passedModels, _ := selectVerifiedModels(results)
	if len(passedModels) == 0 && probeErr == nil {
		probeErr = errors.New("没有模型通过连通性检测")
	}
	if probeErr == nil && !isGPT56MappingEligible(channel) {
		if channel.InternalChannelID == nil {
			probeErr = createInternalChannel(channel, group)
		} else {
			probeErr = syncInternalChannel(channel, group)
		}
	}
	summary := verificationSummary(results, probeErr)
	if probeErr != nil {
		status = marketplacedomain.VerificationFailed
		summary = verificationSummary(results, probeErr)
	}
	connectivityStatus := status
	if isGPT56MappingEligible(channel) {
		status, lifecycle = requiredVerificationState(channel)
	} else if connectivityStatus == marketplacedomain.VerificationPassed {
		lifecycle = marketplacedomain.LifecycleActive
	}
	hash := sha256.Sum256([]byte(run.ID + channel.ID + status + now.Format(time.RFC3339Nano)))
	_ = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		runUpdate := tx.Model(run).Where("status = ?", marketplacedomain.VerificationRunning).Updates(map[string]any{
			"status": connectivityStatus, "stage": "protocol", "summary": summary,
			"evidence_hash": hex.EncodeToString(hash[:]), "completed_at": now, "expires_at": expires,
		})
		if runUpdate.Error != nil {
			return runUpdate.Error
		}
		if runUpdate.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		updates := map[string]any{"verification_status": status, "lifecycle_status": lifecycle}
		if status == marketplacedomain.VerificationPassed {
			updates["verification_due_at"] = expires
			updates["published_at"] = now
		}
		if err := tx.Model(group).Updates(updates).Error; err != nil {
			return err
		}
		channelUpdates := map[string]any{
			"status":                       lifecycle,
			"connectivity_test_status":     connectivityStatus,
			"connectivity_test_checked_at": now,
		}
		return tx.Model(channel).Updates(channelUpdates).Error
	})
}

func requiredVerificationState(channel *marketplaceschema.Channel) (string, string) {
	if isGPT56MappingEligible(channel) {
		switch channel.GPT56MappingStatus {
		case GPT56MappingStatusMatched:
			return marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive
		case GPT56MappingStatusRunning:
			return marketplacedomain.VerificationRunning, marketplacedomain.LifecycleVerifying
		case GPT56MappingStatusPaused:
			return marketplacedomain.VerificationPaused, marketplacedomain.LifecycleDraft
		case GPT56MappingStatusMismatch, GPT56MappingStatusInsufficientEvidence:
			return marketplacedomain.VerificationFailed, marketplacedomain.LifecycleDraft
		default:
			return marketplacedomain.VerificationQueued, marketplacedomain.LifecycleDraft
		}
	}
	switch channel.ConnectivityTestStatus {
	case marketplacedomain.VerificationPassed:
		return marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive
	case marketplacedomain.VerificationRunning:
		return marketplacedomain.VerificationRunning, marketplacedomain.LifecycleVerifying
	case marketplacedomain.VerificationPaused:
		return marketplacedomain.VerificationPaused, marketplacedomain.LifecycleDraft
	case marketplacedomain.VerificationFailed:
		return marketplacedomain.VerificationFailed, marketplacedomain.LifecycleDraft
	default:
		return marketplacedomain.VerificationQueued, marketplacedomain.LifecycleDraft
	}
}

func verifyDeclaredModels(declared, upstream []string) error {
	available := make(map[string]struct{}, len(upstream))
	for _, model := range upstream {
		available[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	missing := make([]string, 0)
	for _, model := range declared {
		if _, ok := available[strings.ToLower(strings.TrimSpace(model))]; !ok {
			missing = append(missing, model)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if len(missing) > 3 {
		missing = missing[:3]
	}
	return errors.New("声明模型未出现在上游模型列表: " + strings.Join(missing, ", "))
}

func selectProbeModel(models []string) string {
	for _, model := range models {
		if !strings.Contains(strings.ToLower(model), "auto-review") {
			return model
		}
	}
	return models[0]
}

func LatestVerification(channelID string) (*marketplaceschema.VerificationRun, error) {
	var run marketplaceschema.VerificationRun
	err := platformdb.DB.Where("channel_id = ?", channelID).Order("created_at desc").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &run, err
}
