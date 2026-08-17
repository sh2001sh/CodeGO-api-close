package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

const (
	GPT56MappingStatusRunning              = "running"
	GPT56MappingStatusMatched              = "matched"
	GPT56MappingStatusMismatch             = "mismatch"
	GPT56MappingStatusInsufficientEvidence = "insufficient_evidence"
	GPT56MappingSampleStatusError          = "error"
	GPT56MappingSampleStatusMissingModel   = "missing_model"
	GPT56MappingSchedulerInterval          = time.Hour
	gpt56MappingSampleCount                = 3
)

var (
	gpt56MappingModels = []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	gpt56SchedulerOnce sync.Once
	gpt56MappingLock   sync.Mutex
)

func isGPT56MappingEligible(channel *marketplaceschema.Channel) bool {
	return len(gpt56MappingModelsForChannel(channel)) > 0
}

func gpt56MappingModelsForChannel(channel *marketplaceschema.Channel) []string {
	if channel == nil {
		return nil
	}
	supported := make(map[string]string, len(gpt56MappingModels))
	for _, model := range gpt56MappingModels {
		supported[model] = model
	}
	models := make([]string, 0, len(gpt56MappingModels))
	seen := make(map[string]struct{}, len(gpt56MappingModels))
	for _, declared := range decodeModels(channel.DeclaredModels) {
		model, ok := supported[strings.ToLower(strings.TrimSpace(declared))]
		if !ok {
			continue
		}
		if _, duplicate := seen[model]; duplicate {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

func runGPT56MappingCheck(channel *marketplaceschema.Channel) (bool, error) {
	if !isGPT56MappingEligible(channel) {
		return false, nil
	}
	gpt56MappingLock.Lock()
	defer gpt56MappingLock.Unlock()
	if channel.GPT56MappingStatus == GPT56MappingStatusRunning {
		return true, nil
	}
	if err := markGPT56MappingCheckRunning(channel.ID); err != nil {
		return true, err
	}
	baseURL, credential, err := gpt56MappingCredentials(channel)
	if err != nil {
		return true, saveGPT56MappingCheck(
			channel.ID,
			unavailableGPT56MappingResults(gpt56MappingModelsForChannel(channel)),
			GPT56MappingStatusInsufficientEvidence,
		)
	}
	results, err := probeGPT56MappingsWithProgress(
		channel.ProviderType,
		baseURL,
		credential,
		gpt56MappingModelsForChannel(channel),
		func(progress []GPT56MappingResult) error {
			return saveGPT56MappingProgress(channel.ID, progress)
		},
	)
	if err != nil {
		return true, err
	}
	return true, saveGPT56MappingCheck(channel.ID, results, gpt56MappingStatus(results))
}

func markGPT56MappingCheckRunning(channelID string) error {
	return platformdb.DB.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"gpt56_mapping_results": "[]", "gpt56_mapping_status": GPT56MappingStatusRunning, "gpt56_mapping_checked_at": nil,
	}).Error
}

func gpt56MappingCredentials(channel *marketplaceschema.Channel) (string, string, error) {
	baseURL, err := platformsecurity.DecryptSecret(channel.BaseURLCiphertext)
	if err != nil {
		return "", "", err
	}
	credential, err := platformsecurity.DecryptSecret(channel.CredentialCiphertext)
	if err != nil {
		return "", "", err
	}
	return baseURL, credential, nil
}

func unavailableGPT56MappingResults(models []string) []GPT56MappingResult {
	results := make([]GPT56MappingResult, 0, len(models))
	for _, model := range models {
		results = append(results, GPT56MappingResult{
			RequestedModel: model,
			Status:         GPT56MappingStatusInsufficientEvidence,
			SampleCount:    gpt56MappingSampleCount,
			Error:          "渠道凭据不可用",
			TestedAt:       time.Now().UTC(),
		})
	}
	return results
}

func probeGPT56Mappings(provider, baseURL, credential string, models []string) []GPT56MappingResult {
	results, _ := probeGPT56MappingsWithProgress(provider, baseURL, credential, models, nil)
	return results
}

func probeGPT56MappingModel(provider, baseURL, credential, model string) GPT56MappingResult {
	result, _ := probeGPT56MappingModelWithProgress(provider, baseURL, credential, model, nil)
	return result
}

func probeGPT56MappingsWithProgress(
	provider, baseURL, credential string,
	models []string,
	onProgress func([]GPT56MappingResult) error,
) ([]GPT56MappingResult, error) {
	results := make([]GPT56MappingResult, len(models))
	for index, model := range models {
		results[index] = GPT56MappingResult{
			RequestedModel: model,
			Status:         GPT56MappingStatusRunning,
			SampleCount:    gpt56MappingSampleCount,
			TestedAt:       time.Now().UTC(),
		}
	}
	for index, model := range models {
		result, err := probeGPT56MappingModelWithProgress(provider, baseURL, credential, model, func(progress GPT56MappingResult) error {
			results[index] = progress
			if onProgress == nil {
				return nil
			}
			return onProgress(results)
		})
		if err != nil {
			return results, err
		}
		results[index] = result
	}
	return results, nil
}

func probeGPT56MappingModelWithProgress(
	provider, baseURL, credential, model string,
	onProgress func(GPT56MappingResult) error,
) (GPT56MappingResult, error) {
	result := GPT56MappingResult{
		RequestedModel: model,
		Status:         GPT56MappingStatusRunning,
		SampleCount:    gpt56MappingSampleCount,
		Samples:        make([]GPT56MappingSample, 0, gpt56MappingSampleCount),
		TestedAt:       time.Now().UTC(),
	}
	reportedModels := make([]string, 0, gpt56MappingSampleCount)
	errorsSeen := make([]string, 0, gpt56MappingSampleCount)
	for sample := 0; sample < gpt56MappingSampleCount; sample++ {
		latencyMS, reported, probeErr := probeMarketplaceInferenceReportedModel(provider, baseURL, credential, model)
		result.LatencyMS += latencyMS
		mappingSample := buildGPT56MappingSample(sample+1, model, reported, latencyMS, probeErr)
		result.Samples = append(result.Samples, mappingSample)
		if mappingSample.Error != "" {
			errorsSeen = append(errorsSeen, mappingSample.Error)
		}
		if mappingSample.ReportedModel != "" {
			reportedModels = append(reportedModels, mappingSample.ReportedModel)
		}
		if mappingSample.Status == GPT56MappingStatusMatched {
			result.MatchedSamples++
		}
		if onProgress != nil {
			if err := onProgress(result); err != nil {
				return result, err
			}
		}
	}
	if result.SampleCount > 0 {
		result.LatencyMS /= int64(result.SampleCount)
	}
	result.ReportedModel = strings.Join(normalizeModels(reportedModels), ", ")
	switch {
	case hasMismatchedReportedModel(model, reportedModels):
		result.Status = GPT56MappingStatusMismatch
	case result.MatchedSamples == result.SampleCount:
		result.Status = GPT56MappingStatusMatched
	default:
		result.Status = GPT56MappingStatusInsufficientEvidence
		result.Error = strings.Join(normalizeModels(errorsSeen), "; ")
	}
	return result, nil
}

func buildGPT56MappingSample(index int, expected, reported string, latencyMS int64, probeErr error) GPT56MappingSample {
	sample := GPT56MappingSample{Index: index, LatencyMS: latencyMS, TestedAt: time.Now().UTC()}
	if probeErr != nil {
		sample.Status = GPT56MappingSampleStatusError
		sample.Error = truncateVerificationError(probeErr.Error())
		return sample
	}
	sample.ReportedModel = strings.TrimSpace(reported)
	if sample.ReportedModel == "" {
		sample.Status = GPT56MappingSampleStatusMissingModel
		sample.Error = "上游响应未返回模型标识"
		return sample
	}
	if sameModelID(expected, sample.ReportedModel) {
		sample.Status = GPT56MappingStatusMatched
	} else {
		sample.Status = GPT56MappingStatusMismatch
	}
	return sample
}

func hasMismatchedReportedModel(expected string, reported []string) bool {
	for _, model := range reported {
		if !sameModelID(expected, model) {
			return true
		}
	}
	return false
}

func sameModelID(expected, actual string) bool {
	return strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(actual))
}

func gpt56MappingStatus(results []GPT56MappingResult) string {
	if len(results) == 0 {
		return GPT56MappingStatusInsufficientEvidence
	}
	matched := 0
	for _, result := range results {
		if result.Status == GPT56MappingStatusMismatch {
			return GPT56MappingStatusMismatch
		}
		if result.Status == GPT56MappingStatusMatched {
			matched++
		}
	}
	if matched == len(results) {
		return GPT56MappingStatusMatched
	}
	return GPT56MappingStatusInsufficientEvidence
}

func saveGPT56MappingCheck(channelID string, results []GPT56MappingResult, status string) error {
	encoded, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return platformdb.DB.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"gpt56_mapping_results": string(encoded), "gpt56_mapping_status": status, "gpt56_mapping_checked_at": now,
	}).Error
}

func saveGPT56MappingProgress(channelID string, results []GPT56MappingResult) error {
	encoded, err := json.Marshal(results)
	if err != nil {
		return err
	}
	return platformdb.DB.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"gpt56_mapping_results": string(encoded), "gpt56_mapping_status": GPT56MappingStatusRunning,
	}).Error
}

func decodeGPT56MappingResults(raw string) []GPT56MappingResult {
	var results []GPT56MappingResult
	if json.Unmarshal([]byte(raw), &results) != nil || results == nil {
		return []GPT56MappingResult{}
	}
	return results
}

func publicGPT56MappingResults(raw string) []GPT56MappingResult {
	results := decodeGPT56MappingResults(raw)
	for resultIndex := range results {
		results[resultIndex].Error = ""
		for sampleIndex := range results[resultIndex].Samples {
			sample := &results[resultIndex].Samples[sampleIndex]
			switch sample.Status {
			case GPT56MappingSampleStatusError:
				sample.Error = "请求失败，未获得可验证结果"
			case GPT56MappingSampleStatusMissingModel:
				sample.Error = "上游响应未返回模型标识"
			default:
				sample.Error = ""
			}
		}
	}
	return results
}

// StartGPT56MappingScheduler checks eligible GPT-5.6 channels once per day.
func StartGPT56MappingScheduler(ctx context.Context) {
	gpt56SchedulerOnce.Do(func() {
		go func() {
			runDueGPT56MappingChecks(ctx)
			ticker := time.NewTicker(GPT56MappingSchedulerInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runDueGPT56MappingChecks(ctx)
				}
			}
		}()
	})
}

func runDueGPT56MappingChecks(ctx context.Context) {
	if platformdb.DB == nil || ctx.Err() != nil {
		return
	}
	var channels []marketplaceschema.Channel
	dueBefore := time.Now().UTC().Add(-24 * time.Hour)
	if err := platformdb.DB.Where(
		"(declared_models LIKE ? OR declared_models LIKE ? OR declared_models LIKE ?) AND (gpt56_mapping_checked_at IS NULL OR gpt56_mapping_checked_at <= ?)",
		"%gpt-5.6-sol%", "%gpt-5.6-terra%", "%gpt-5.6-luna%", dueBefore,
	).Find(&channels).Error; err != nil {
		platformobservability.SysError("load due GPT-5.6 mapping checks: " + err.Error())
		return
	}
	for index := range channels {
		if _, err := runGPT56MappingCheck(&channels[index]); err != nil {
			platformobservability.SysError(fmt.Sprintf("run GPT-5.6 mapping check channel=%s: %s", channels[index].ID, err.Error()))
		}
	}
}
