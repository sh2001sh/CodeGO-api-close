package app

import (
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
		_, listed := available[strings.ToLower(strings.TrimSpace(model))]
		latencyMS, err := probeMarketplaceInferenceTimed(provider, baseURL, apiKey, model)
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

func partialVerificationSummary(passed []string, results []ModelVerificationResult, rejected []string) string {
	const maxDisplayedRejected = 5
	displayed := rejected
	if len(displayed) > maxDisplayedRejected {
		displayed = displayed[:maxDisplayedRejected]
	}
	suffix := ""
	if len(rejected) > len(displayed) {
		suffix = fmt.Sprintf(" 等 %d 个", len(rejected))
	}
	return fmt.Sprintf(
		"%d/%d 个模型检测通过并上架；已自动剔除未通过模型: %s%s",
		len(passed), len(results), strings.Join(displayed, ", "), suffix,
	)
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
