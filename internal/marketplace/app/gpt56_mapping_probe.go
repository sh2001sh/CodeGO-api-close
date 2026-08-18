package app

import (
	"strings"
	"time"
)

func probeGPT56MappingsWithProgress(
	provider, baseURL, credential string,
	models []string,
	policy gpt56DetectionPolicy,
	onProgress func([]GPT56MappingResult) error,
) ([]GPT56MappingResult, error) {
	results := make([]GPT56MappingResult, len(models))
	for index, model := range models {
		results[index] = GPT56MappingResult{
			RequestedModel: model,
			Status:         GPT56MappingStatusRunning,
			SampleCount:    policy.sampleCount(),
			TestedAt:       time.Now().UTC(),
		}
	}
	for index, model := range models {
		result, err := probeGPT56MappingModelWithProgress(
			provider, baseURL, credential, model, policy,
			func(progress GPT56MappingResult) error {
				results[index] = progress
				if onProgress == nil {
					return nil
				}
				return onProgress(results)
			},
		)
		if err != nil {
			return results, err
		}
		results[index] = result
		if result.Status == GPT56MappingStatusMismatch {
			for skipped := index + 1; skipped < len(results); skipped++ {
				results[skipped] = GPT56MappingResult{
					RequestedModel: models[skipped],
					Status:         GPT56MappingStatusInsufficientEvidence,
					SampleCount:    policy.sampleCount(),
					Error:          "前序模型已确认映射不一致，后续模型未继续检测",
					TestedAt:       time.Now().UTC(),
				}
			}
			break
		}
	}
	return results, nil
}

func probeGPT56MappingModelWithProgress(
	provider, baseURL, credential, model string,
	policy gpt56DetectionPolicy,
	onProgress func(GPT56MappingResult) error,
) (GPT56MappingResult, error) {
	sampleCount := policy.sampleCount()
	result := GPT56MappingResult{
		RequestedModel: model,
		Status:         GPT56MappingStatusRunning,
		SampleCount:    sampleCount,
		Samples:        make([]GPT56MappingSample, 0, sampleCount),
		TestedAt:       time.Now().UTC(),
	}
	reportedModels := make([]string, 0, sampleCount)
	errorsSeen := make([]string, 0, sampleCount)
	mismatchedSamples := 0
stop:
	for _, variant := range policy.Variants {
		for repetition := 0; repetition < policy.Repetitions; repetition++ {
			latencyMS, reported, probeErr := probeMarketplaceInferenceReportedModelWithVariant(
				provider, baseURL, credential, model, variant,
			)
			result.LatencyMS += latencyMS
			sample := buildGPT56MappingSample(
				len(result.Samples)+1, variant.Name, model, reported, latencyMS, probeErr,
			)
			result.Samples = append(result.Samples, sample)
			if sample.Status == GPT56MappingSampleStatusError && sample.Error != "" {
				errorsSeen = append(errorsSeen, sample.Error)
			}
			if sample.ReportedModel != "" {
				reportedModels = append(reportedModels, sample.ReportedModel)
			}
			if sample.Status == GPT56MappingStatusMatched {
				result.MatchedSamples++
			}
			if sample.Status == GPT56MappingStatusMismatch {
				mismatchedSamples++
			}
			if onProgress != nil {
				if err := onProgress(result); err != nil {
					return result, err
				}
			}
			if mismatchedSamples*2 > result.SampleCount {
				break stop
			}
		}
	}
	if len(result.Samples) > 0 {
		result.LatencyMS /= int64(len(result.Samples))
	}
	result.ReportedModel = strings.Join(normalizeModels(reportedModels), ", ")
	switch {
	case mismatchedSamples*2 > result.SampleCount:
		result.Status = GPT56MappingStatusMismatch
	case mismatchedSamples > 0 || len(errorsSeen) > 0:
		result.Status = GPT56MappingStatusInsufficientEvidence
	default:
		result.Status = GPT56MappingStatusMatched
	}
	result.Error = strings.Join(normalizeModels(errorsSeen), "; ")
	return result, nil
}

func buildGPT56MappingSample(
	index int,
	variant, expected, reported string,
	latencyMS int64,
	probeErr error,
) GPT56MappingSample {
	sample := GPT56MappingSample{
		Index: index, Variant: variant, LatencyMS: latencyMS, TestedAt: time.Now().UTC(),
	}
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
