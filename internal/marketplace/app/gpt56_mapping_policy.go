package app

import (
	"strings"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
)

const (
	GPT56MappingStatusRunning              = "running"
	GPT56MappingStatusMatched              = "matched"
	GPT56MappingStatusMismatch             = "mismatch"
	GPT56MappingStatusInsufficientEvidence = "insufficient_evidence"
	GPT56MappingSampleStatusError          = "error"
	GPT56MappingSampleStatusMissingModel   = "missing_model"

	GPT56MappingLevelDailyLight   = "daily_light"
	GPT56MappingLevelConfirmation = "confirmation"

	GPT56MappingTriggerScheduled    = "scheduled"
	GPT56MappingTriggerManual       = "manual"
	GPT56MappingTriggerInitial      = "initial"
	GPT56MappingTriggerConfirmation = "confirmation"

	GPT56MappingSchedulerInterval = time.Hour
	gpt56MappingHistoryLimit      = 5
)

type gpt56ProbeVariant struct {
	Name            string
	Prompt          string
	MaxOutputTokens int
}

type gpt56DetectionPolicy struct {
	Level             string
	Repetitions       int
	Variants          []gpt56ProbeVariant
	ConfirmOnMismatch bool
}

var gpt56MappingModels = []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}

var gpt56ProbeVariants = []gpt56ProbeVariant{
	{Name: "exact_reply", Prompt: "Reply with exactly OK.", MaxOutputTokens: 8},
	{Name: "short_answer", Prompt: "Answer only with the word READY.", MaxOutputTokens: 8},
	{Name: "simple_fact", Prompt: "What is 2 + 2? Reply with only the number.", MaxOutputTokens: 8},
}

func gpt56Policy(level string) gpt56DetectionPolicy {
	if level == GPT56MappingLevelDailyLight {
		return gpt56DetectionPolicy{
			Level: level, Repetitions: 3, Variants: gpt56ProbeVariants, ConfirmOnMismatch: true,
		}
	}
	return gpt56DetectionPolicy{
		Level: GPT56MappingLevelConfirmation, Repetitions: 10, Variants: gpt56ProbeVariants,
	}
}

func (policy gpt56DetectionPolicy) sampleCount() int {
	return policy.Repetitions * len(policy.Variants)
}

func shouldConfirmGPT56Mapping(policy gpt56DetectionPolicy, status string) bool {
	return policy.ConfirmOnMismatch && status == GPT56MappingStatusMismatch
}

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

func sameModelID(expected, actual string) bool {
	return strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(actual))
}
