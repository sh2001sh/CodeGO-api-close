package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
)

func applyModelConsistencyStatus(channel *marketplaceschema.Channel, status string) error {
	status = strings.TrimSpace(status)
	switch status {
	case "", marketplacedomain.ModelConsistencyPassed,
		marketplacedomain.ModelConsistencyFailed,
		marketplacedomain.ModelConsistencyQuestioned:
		channel.ModelConsistencyStatus = status
		return nil
	default:
		return errors.New("模型一致性标注无效")
	}
}

func probeDeclaredModels(
	ctx context.Context,
	provider string,
	baseURL string,
	apiKey string,
	declared []string,
	upstream []string,
	report func([]ModelVerificationResult),
) ([]ModelVerificationResult, error) {
	available := make(map[string]struct{}, len(upstream))
	for _, model := range upstream {
		available[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	results := make([]ModelVerificationResult, 0, len(declared))
	failedModels := make([]string, 0)
	for _, model := range declared {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		_, listed := available[strings.ToLower(strings.TrimSpace(model))]
		latencyMS, err := probeMarketplaceInferenceTimedContext(ctx, provider, baseURL, apiKey, model)
		result := ModelVerificationResult{
			Model: model, Status: marketplacedomain.ModelVerificationPassed,
			Listed: listed, LatencyMS: latencyMS, TestedAt: time.Now().UTC(),
		}
		if err != nil {
			result.Status = marketplacedomain.ModelVerificationFailed
			result.Error = truncateVerificationError(err.Error())
			failedModels = append(failedModels, model)
		}
		results = append(results, result)
		if report != nil {
			report(append([]ModelVerificationResult(nil), results...))
		}
	}
	if len(failedModels) == 0 {
		return results, nil
	}
	return results, fmt.Errorf("模型连通性检测失败: %s", strings.Join(failedModels, ", "))
}

func encodeModelVerificationResults(results []ModelVerificationResult) string {
	encoded, err := json.Marshal(results)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func decodeModelVerificationResults(raw string) []ModelVerificationResult {
	var results []ModelVerificationResult
	if json.Unmarshal([]byte(raw), &results) != nil || results == nil {
		return []ModelVerificationResult{}
	}
	return results
}

func failedModelVerificationModels(declared []string, results []ModelVerificationResult) []string {
	byModel := modelVerificationResultsByModel(results)
	failed := make([]string, 0)
	for _, model := range normalizeModels(declared) {
		result, ok := byModel[strings.ToLower(model)]
		if ok && (!result.Listed || result.Status != marketplacedomain.ModelVerificationPassed) {
			failed = append(failed, model)
		}
	}
	return failed
}

func retainModelVerificationResults(declared []string, results []ModelVerificationResult, retried []string) []ModelVerificationResult {
	retriedModels := make(map[string]struct{}, len(retried))
	for _, model := range retried {
		retriedModels[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	retained := make([]ModelVerificationResult, 0, len(results))
	for _, result := range mergeModelVerificationResults(declared, results, nil) {
		if _, retrying := retriedModels[strings.ToLower(strings.TrimSpace(result.Model))]; !retrying {
			retained = append(retained, result)
		}
	}
	return retained
}

// pendingModelVerificationModels returns only declared models without any
// persisted result. Existing failures are intentionally retained until the
// owner explicitly retries failed models.
func pendingModelVerificationModels(declared []string, results []ModelVerificationResult) []string {
	byModel := modelVerificationResultsByModel(results)
	pending := make([]string, 0)
	for _, model := range normalizeModels(declared) {
		if _, ok := byModel[strings.ToLower(model)]; !ok {
			pending = append(pending, model)
		}
	}
	return pending
}

func allModelsVerified(declared []string, results []ModelVerificationResult) bool {
	byModel := modelVerificationResultsByModel(results)
	for _, model := range normalizeModels(declared) {
		result, ok := byModel[strings.ToLower(model)]
		if !ok || !result.Listed || result.Status != marketplacedomain.ModelVerificationPassed {
			return false
		}
	}
	return true
}

func mergeModelVerificationResults(declared []string, previous, updates []ModelVerificationResult) []ModelVerificationResult {
	byModel := modelVerificationResultsByModel(previous)
	for _, result := range updates {
		byModel[strings.ToLower(strings.TrimSpace(result.Model))] = result
	}
	merged := make([]ModelVerificationResult, 0, len(byModel))
	for _, model := range normalizeModels(declared) {
		if result, ok := byModel[strings.ToLower(model)]; ok {
			result.Model = model
			merged = append(merged, result)
		}
	}
	return merged
}

func modelVerificationResultsByModel(results []ModelVerificationResult) map[string]ModelVerificationResult {
	byModel := make(map[string]ModelVerificationResult, len(results))
	for _, result := range results {
		if model := strings.ToLower(strings.TrimSpace(result.Model)); model != "" {
			byModel[model] = result
		}
	}
	return byModel
}

func publicModelVerificationResults(raw string) []ModelVerificationResult {
	results := decodeModelVerificationResults(raw)
	for index := range results {
		results[index].Error = ""
	}
	return results
}

func verificationSummary(results []ModelVerificationResult, probeErr error) string {
	passedModels, _ := selectVerifiedModels(results)
	passed := len(passedModels)
	if probeErr == nil {
		return fmt.Sprintf("%d/%d 个模型连通性检测通过，渠道已自动上架", passed, len(results))
	}
	return fmt.Sprintf("%d/%d 个模型连通性检测通过；%s", passed, len(results), probeErr.Error())
}

func selectVerifiedModels(results []ModelVerificationResult) ([]string, []string) {
	passed := make([]string, 0, len(results))
	rejected := make([]string, 0)
	for _, result := range results {
		if result.Listed && result.Status == marketplacedomain.ModelVerificationPassed {
			passed = append(passed, result.Model)
			continue
		}
		rejected = append(rejected, result.Model)
	}
	return normalizeModels(passed), normalizeModels(rejected)
}

func truncateVerificationError(message string) string {
	message = strings.TrimSpace(message)
	const maxRunes = 300
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes]) + "..."
}
