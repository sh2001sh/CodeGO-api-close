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
	GPT56MappingSchedulerInterval          = time.Hour
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
	if channel == nil || (channel.SubmittedSourceLabel != "Codex Plus" && channel.SubmittedSourceLabel != "Codex Pro") {
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
		return true, saveGPT56MappingCheck(channel.ID, []GPT56MappingResult{unavailableGPT56MappingResult()}, GPT56MappingStatusInsufficientEvidence)
	}
	results := probeGPT56Mappings(channel.ProviderType, baseURL, credential, gpt56MappingModelsForChannel(channel))
	return true, saveGPT56MappingCheck(channel.ID, results, gpt56MappingStatus(results))
}

func markGPT56MappingCheckRunning(channelID string) error {
	return platformdb.DB.Model(&marketplaceschema.Channel{}).Where("id = ?", channelID).Update("gpt56_mapping_status", GPT56MappingStatusRunning).Error
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

func unavailableGPT56MappingResult() GPT56MappingResult {
	return GPT56MappingResult{Status: GPT56MappingStatusInsufficientEvidence, Error: "渠道凭据不可用", TestedAt: time.Now().UTC()}
}

func probeGPT56Mappings(provider, baseURL, credential string, models []string) []GPT56MappingResult {
	results := make([]GPT56MappingResult, 0, len(models))
	for _, model := range models {
		latencyMS, reported, probeErr := probeMarketplaceInferenceReportedModel(provider, baseURL, credential, model)
		result := GPT56MappingResult{RequestedModel: model, ReportedModel: reported, LatencyMS: latencyMS, TestedAt: time.Now().UTC()}
		switch {
		case probeErr != nil:
			result.Status = GPT56MappingStatusInsufficientEvidence
			result.Error = truncateVerificationError(probeErr.Error())
		case sameModelID(model, reported):
			result.Status = GPT56MappingStatusMatched
		case strings.TrimSpace(reported) == "":
			result.Status = GPT56MappingStatusInsufficientEvidence
			result.Error = "上游响应未返回模型标识"
		default:
			result.Status = GPT56MappingStatusMismatch
		}
		results = append(results, result)
	}
	return results
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

func decodeGPT56MappingResults(raw string) []GPT56MappingResult {
	var results []GPT56MappingResult
	if json.Unmarshal([]byte(raw), &results) != nil || results == nil {
		return []GPT56MappingResult{}
	}
	return results
}

// StartGPT56MappingScheduler checks eligible Codex Plus and Pro channels once per day.
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
	if err := platformdb.DB.Where("submitted_source_label IN ? AND (gpt56_mapping_checked_at IS NULL OR gpt56_mapping_checked_at <= ?)", []string{"Codex Plus", "Codex Pro"}, dueBefore).Find(&channels).Error; err != nil {
		platformobservability.SysError("load due GPT-5.6 mapping checks: " + err.Error())
		return
	}
	for index := range channels {
		if _, err := runGPT56MappingCheck(&channels[index]); err != nil {
			platformobservability.SysError(fmt.Sprintf("run GPT-5.6 mapping check channel=%s: %s", channels[index].ID, err.Error()))
		}
	}
}
