package app

import (
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	"github.com/stretchr/testify/require"
)

func TestPendingModelVerificationModelsOnlyReturnsNewModels(t *testing.T) {
	checkedAt := time.Now().UTC()
	results := []ModelVerificationResult{
		{Model: "gpt-5", Listed: true, Status: marketplacedomain.ModelVerificationPassed, TestedAt: checkedAt},
		{Model: "gpt-5-mini", Listed: false, Status: marketplacedomain.ModelVerificationFailed, TestedAt: checkedAt},
	}

	pending := pendingModelVerificationModels(
		[]string{"gpt-5", "gpt-5-mini", "gpt-5.1"},
		results,
	)

	require.Equal(t, []string{"gpt-5.1"}, pending)
}

func TestAllModelsVerifiedRequiresEveryDeclaredModel(t *testing.T) {
	results := []ModelVerificationResult{
		{Model: "gpt-5", Listed: true, Status: marketplacedomain.ModelVerificationPassed},
		{Model: "gpt-5-mini", Listed: false, Status: marketplacedomain.ModelVerificationFailed},
	}
	require.False(t, allModelsVerified([]string{"gpt-5", "gpt-5-mini"}, results))
	results[1] = ModelVerificationResult{Model: "gpt-5-mini", Listed: true, Status: marketplacedomain.ModelVerificationPassed}
	require.True(t, allModelsVerified([]string{"gpt-5", "gpt-5-mini"}, results))
}

func TestModelVerificationResultsRetainSuccessfulModelsWhenAddingModel(t *testing.T) {
	checkedAt := time.Now().UTC()
	previous := []ModelVerificationResult{{
		Model: "gpt-5", Listed: true, Status: marketplacedomain.ModelVerificationPassed, TestedAt: checkedAt,
	}}

	retained := mergeModelVerificationResults([]string{"gpt-5", "gpt-5.1"}, previous, nil)

	require.Len(t, retained, 1)
	require.Equal(t, "gpt-5", retained[0].Model)
	require.Equal(t, marketplacedomain.ModelVerificationPassed, retained[0].Status)
}
