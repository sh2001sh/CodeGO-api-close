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
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	var moderationErr error
	ctx, moderationErr = preparePresentationProbe(ctx, channel)
	if moderationErr != nil {
		return nil, moderationErr
	}
	if reportStage != nil {
		reportStage("basic_security")
	}
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
	if reportStage != nil {
		reportStage("model_list")
	}
	models, err := fetchUpstreamModelsContext(ctx, channel.ProviderType, baseURL, credential)
	if err != nil {
		return nil, err
	}
	declared := normalizeModels(targetModels)
	if len(declared) == 0 {
		declared = decodeModels(channel.DeclaredModels)
	}
	if reportStage != nil {
		reportStage("model_match")
	}
	modelListErr := verifyDeclaredModels(declared, models)
	if reportStage != nil {
		reportStage("inference")
	}
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
		return nil, errors.New("上游模型列表请求地址无效")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("上游模型列表请求地址无效")
	}
	setMarketplaceAuthHeaders(httpReq, provider, apiKey)
	response, err := marketplaceUpstreamHTTPClient.Do(httpReq)
	if err != nil {
		return nil, marketplaceUpstreamRequestError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("获取模型列表失败（HTTP %d）", response.StatusCode)
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
	passedModels, rejectedModels := selectVerifiedModels(results)
	if len(passedModels) == 0 && probeErr == nil {
		probeErr = errors.New("没有模型通过连通性检测")
	}
	if len(rejectedModels) > 0 && probeErr == nil {
		probeErr = fmt.Errorf("%d 个声明模型未通过连通性检测", len(rejectedModels))
	}
	summary := verificationSummary(results, probeErr)
	if probeErr != nil {
		status = marketplacedomain.VerificationFailed
		summary = verificationSummary(results, probeErr)
	}
	connectivityStatus := status
	if connectivityStatus == marketplacedomain.VerificationPassed {
		lifecycle = marketplacedomain.LifecycleActive
	}
	// A manual connectivity test must not unpublish a channel that is already
	// serving traffic.
	if probeErr == nil && connectivityStatus == marketplacedomain.VerificationPassed &&
		(channel.Status == marketplacedomain.LifecycleActive ||
			group.LifecycleStatus == marketplacedomain.LifecycleActive) {
		status = marketplacedomain.VerificationPassed
		lifecycle = marketplacedomain.LifecycleActive
	}
	hash := sha256.Sum256([]byte(run.ID + channel.ID + status + now.Format(time.RFC3339Nano)))
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		// Match queue/edit lock order. Recheck lifecycle and run before any
		// internal routing side effect; a cancelled or disabled run cannot
		// publish a gateway channel using its old in-memory snapshot.
		var currentChannel marketplaceschema.Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentChannel, "id = ?", channel.ID).Error; err != nil {
			return err
		}
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
		var currentGroup marketplaceschema.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentGroup, "id = ?", group.ID).Error; err != nil {
			return err
		}
		*channel, *group = currentChannel, currentGroup
		if marketplaceChannelAdminHeld(channel) || group.LifecycleStatus == marketplacedomain.LifecycleDisabled {
			channel.Status, group.LifecycleStatus = marketplacedomain.LifecycleDisabled, marketplacedomain.LifecycleDisabled
		}
		paused := channel.Status == marketplacedomain.LifecycleSuspended || group.LifecycleStatus == marketplacedomain.LifecycleSuspended || marketplaceChannelAdminHeld(channel)
		if probeErr == nil && !paused {
			var syncErr error
			if channel.InternalChannelID == nil {
				syncErr = createInternalChannelWithDB(tx, channel, group)
			} else {
				syncErr = syncInternalChannelWithDB(tx, channel, group)
			}
			if syncErr != nil {
				return syncErr
			}
			if err := syncPausedMarketplaceChannelWithDB(tx, channel, false); err != nil {
				return err
			}
		}
		updates := map[string]any{"verification_status": status, "lifecycle_status": gorm.Expr("CASE WHEN lifecycle_status IN ? THEN lifecycle_status ELSE ? END", []string{marketplacedomain.LifecycleSuspended, marketplacedomain.LifecycleDisabled}, lifecycle)}
		if status == marketplacedomain.VerificationPassed {
			updates["verification_due_at"] = expires
			updates["published_at"] = now
		}
		if err := tx.Model(group).Updates(updates).Error; err != nil {
			return err
		}
		channelUpdates := map[string]any{
			"status":                       gorm.Expr("CASE WHEN status IN ? THEN status ELSE ? END", []string{marketplacedomain.LifecycleSuspended, marketplacedomain.LifecycleDisabled}, lifecycle),
			"connectivity_test_status":     connectivityStatus,
			"connectivity_test_checked_at": now,
		}
		if marketplaceChannelAdminHeld(channel) {
			channelUpdates["status"] = marketplacedomain.LifecycleDisabled
			if err := tx.Model(group).Update("lifecycle_status", marketplacedomain.LifecycleDisabled).Error; err != nil {
				return err
			}
		}
		if paused {
			if err := syncPausedMarketplaceChannelWithDB(tx, channel, true); err != nil {
				return err
			}
		}
		return tx.Model(channel).Updates(channelUpdates).Error
	})
	if err != nil && !errors.Is(err, errVerificationNotRunning) && !errors.Is(err, gorm.ErrRecordNotFound) {
		platformobservability.SysError("complete marketplace verification: " + err.Error())
	}
	if err == nil && channel.InternalChannelID != nil {
		refreshInternalChannelRuntime(*channel.InternalChannelID)
	}
}

func requiredVerificationState(channel *marketplaceschema.Channel) (string, string) {
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
