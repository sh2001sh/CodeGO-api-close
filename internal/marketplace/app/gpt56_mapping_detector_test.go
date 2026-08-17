package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestGPT56MappingEligibilityRequiresSupportedModel(t *testing.T) {
	channel := &marketplaceschema.Channel{
		SubmittedSourceLabel: "Codex Plus",
		DeclaredModels:       `["gpt-5.6-sol","other-model","gpt-5.6-luna"]`,
	}
	require.True(t, isGPT56MappingEligible(channel))
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-5.6-luna"}, gpt56MappingModelsForChannel(channel))

	channel.SubmittedSourceLabel = "CC-Max"
	require.True(t, isGPT56MappingEligible(channel))

	channel.SubmittedSourceLabel = "Codex Pro"
	channel.DeclaredModels = `["gpt-5.6-sol","gpt-5.6-terra"]`
	require.True(t, isGPT56MappingEligible(channel))

	channel.DeclaredModels = `["other-model"]`
	require.False(t, isGPT56MappingEligible(channel))
}

func TestGPT56MappingStatusPrefersMismatch(t *testing.T) {
	require.Equal(t, GPT56MappingStatusMatched, gpt56MappingStatus([]GPT56MappingResult{
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusMatched},
	}))
	require.Equal(t, GPT56MappingStatusMismatch, gpt56MappingStatus([]GPT56MappingResult{
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusMismatch},
		{Status: GPT56MappingStatusInsufficientEvidence},
	}))
	require.Equal(t, GPT56MappingStatusInsufficientEvidence, gpt56MappingStatus([]GPT56MappingResult{
		{Status: GPT56MappingStatusMatched},
		{Status: GPT56MappingStatusInsufficientEvidence},
	}))
}

func TestGPT56MappingPoliciesUseTieredEvidence(t *testing.T) {
	light := gpt56Policy(GPT56MappingLevelDailyLight)
	confirmation := gpt56Policy(GPT56MappingLevelConfirmation)

	require.Equal(t, 9, light.sampleCount())
	require.Equal(t, 30, confirmation.sampleCount())
	require.True(t, shouldConfirmGPT56Mapping(light, GPT56MappingStatusMismatch))
	require.False(t, shouldConfirmGPT56Mapping(light, GPT56MappingStatusInsufficientEvidence))
	require.False(t, shouldConfirmGPT56Mapping(confirmation, GPT56MappingStatusMismatch))
}

func TestGPT56MappingProbeReadsReportedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var requestBody struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&requestBody))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"gpt-5.6-sol","choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	latencyMS, reported, err := probeMarketplaceInferenceReportedModel(
		"openai_compatible", server.URL, "test-key", "gpt-5.6-sol",
	)
	require.NoError(t, err)
	require.GreaterOrEqual(t, latencyMS, int64(0))
	require.Equal(t, "gpt-5.6-sol", reported)
}

func TestGPT56MappingProbeRequiresAllSamplesToMatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		reported := "gpt-5.6-sol"
		if requests == 3 {
			reported = "gpt-5.6-luna"
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"` + reported + `","choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	policy := gpt56Policy(GPT56MappingLevelDailyLight)
	result, err := probeGPT56MappingModelWithProgress(
		"openai_compatible", server.URL, "test-key", "gpt-5.6-sol", policy, nil,
	)
	require.NoError(t, err)
	require.Equal(t, policy.sampleCount(), requests)
	require.Equal(t, GPT56MappingStatusMismatch, result.Status)
	require.Equal(t, policy.sampleCount()-1, result.MatchedSamples)
	require.Equal(t, policy.sampleCount(), result.SampleCount)
	require.Len(t, result.Samples, policy.sampleCount())
	require.Equal(t, GPT56MappingStatusMismatch, result.Samples[2].Status)
	require.Equal(t, "gpt-5.6-luna", result.Samples[2].ReportedModel)
	require.Equal(t, "exact_reply", result.Samples[2].Variant)
}

func TestGPT56MappingProbeReportsEachSampleProgress(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 3 {
			http.Error(writer, "temporary upstream failure", http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"gpt-5.6-terra","choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer server.Close()

	policy := gpt56Policy(GPT56MappingLevelDailyLight)
	progressCounts := make([]int, 0, policy.sampleCount())
	result, err := probeGPT56MappingModelWithProgress(
		"openai_compatible", server.URL, "test-key", "gpt-5.6-terra",
		policy,
		func(progress GPT56MappingResult) error {
			progressCounts = append(progressCounts, len(progress.Samples))
			require.Equal(t, GPT56MappingStatusRunning, progress.Status)
			return nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, progressCounts)
	require.Equal(t, GPT56MappingStatusInsufficientEvidence, result.Status)
	require.Equal(t, policy.sampleCount()-1, result.MatchedSamples)
	require.Equal(t, GPT56MappingSampleStatusError, result.Samples[2].Status)
	require.Contains(t, result.Samples[2].Error, "temporary upstream failure")
}

func TestGPT56MappingHistoryKeepsPreviousRuns(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.GPT56MappingRun{},
	))
	channel := marketplaceschema.Channel{
		ID: "history-channel", OwnerUserID: 1, ProviderType: "openai_compatible",
		DeclaredModels: `["gpt-5.6-sol"]`, Status: "active",
	}
	require.NoError(t, db.Create(&channel).Error)

	first, err := startGPT56MappingRun(
		channel.ID, GPT56MappingLevelDailyLight, GPT56MappingTriggerScheduled, "",
	)
	require.NoError(t, err)
	firstResult := []GPT56MappingResult{{
		RequestedModel: "gpt-5.6-sol", Status: GPT56MappingStatusMatched,
		SampleCount: 9, MatchedSamples: 9, TestedAt: time.Now().UTC(),
	}}
	require.NoError(t, finishGPT56MappingRun(first, firstResult, GPT56MappingStatusMatched))

	second, err := startGPT56MappingRun(
		channel.ID, GPT56MappingLevelConfirmation, GPT56MappingTriggerManual, "",
	)
	require.NoError(t, err)
	secondResult := []GPT56MappingResult{{
		RequestedModel: "gpt-5.6-sol", Status: GPT56MappingStatusInsufficientEvidence,
		SampleCount: 30, TestedAt: time.Now().UTC(),
	}}
	require.NoError(t, finishGPT56MappingRun(
		second, secondResult, GPT56MappingStatusInsufficientEvidence,
	))

	history := latestGPT56MappingRuns(channel.ID, 5)
	require.Len(t, history, 2)
	byID := map[string]GPT56MappingRunView{
		history[0].ID: history[0],
		history[1].ID: history[1],
	}
	require.Equal(t, GPT56MappingLevelConfirmation, byID[second.ID].Level)
	require.Equal(t, GPT56MappingStatusInsufficientEvidence, byID[second.ID].Status)
	require.Equal(t, GPT56MappingLevelDailyLight, byID[first.ID].Level)
	require.Equal(t, GPT56MappingStatusMatched, byID[first.ID].Status)
}

func TestConfirmedGPT56MismatchRemovesGroupFromRouting(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
	))
	channel := marketplaceschema.Channel{
		ID: "mismatch-channel", OwnerUserID: 1, ProviderType: "openai_compatible",
		DeclaredModels: `["gpt-5.6-sol"]`, Status: "active",
	}
	group := marketplaceschema.Group{
		ID: "mismatch-group", ChannelID: channel.ID, OwnerUserID: 1,
		LifecycleStatus: "active", VerificationStatus: "passed",
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	require.NoError(t, applyConfirmedGPT56Mismatch(channel.ID))
	require.NoError(t, db.First(&channel, "id = ?", channel.ID).Error)
	require.NoError(t, db.First(&group, "id = ?", group.ID).Error)
	require.Equal(t, "draft", channel.Status)
	require.Equal(t, "draft", group.LifecycleStatus)
	require.Equal(t, "failed", group.VerificationStatus)
}

func TestScheduledGPT56ChecksOnlyRunForDuePublishedChannels(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.GPT56MappingRun{},
	))
	now := time.Now().UTC()
	channels := []marketplaceschema.Channel{
		{
			ID: "active-due", OwnerUserID: 1, ProviderType: "openai_compatible",
			DeclaredModels: `["gpt-5.6-sol"]`, Status: "active",
		},
		{
			ID: "draft-due", OwnerUserID: 1, ProviderType: "openai_compatible",
			DeclaredModels: `["gpt-5.6-sol"]`, Status: "draft",
		},
		{
			ID: "active-fresh", OwnerUserID: 1, ProviderType: "openai_compatible",
			DeclaredModels: `["gpt-5.6-sol"]`, Status: "active", GPT56MappingCheckedAt: &now,
		},
	}
	for index := range channels {
		require.NoError(t, db.Create(&channels[index]).Error)
	}

	runDueGPT56MappingChecks(context.Background())

	var runs []marketplaceschema.GPT56MappingRun
	require.NoError(t, db.Find(&runs).Error)
	require.Len(t, runs, 1)
	require.Equal(t, "active-due", runs[0].ChannelID)
	require.Equal(t, GPT56MappingLevelDailyLight, runs[0].Level)
	require.Equal(t, GPT56MappingTriggerScheduled, runs[0].Trigger)
}
