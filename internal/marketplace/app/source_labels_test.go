package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceDisplayNameUsesSourceMultiplierAndNumericChannelID(t *testing.T) {
	require.Equal(
		t,
		"123456789012-Codex Plus-0.02x",
		marketplaceDisplayName("Codex Plus", 0.02, "123456789012"),
	)
}

func TestCanonicalSourceLabelSupportsCodexMixedPool(t *testing.T) {
	label, ok := canonicalSourceLabel("codex-mixed")
	require.True(t, ok)
	require.Equal(t, "Codex 混合号池", label)
	require.Equal(t, "Codex-Mixed-abc123", marketplaceInternalGroupName(label, "abc123"))
}

func TestMarketplaceSupportsGrokAndGeminiSources(t *testing.T) {
	for _, source := range []string{"Grok", "Gemini"} {
		label, ok := canonicalSourceLabel(source)
		require.True(t, ok)
		require.Equal(t, source, label)
		require.NoError(t, validateSourceLabel("openai_compatible", source))
		require.Equal(t, source+"-abc123", marketplaceInternalGroupName(source, "abc123"))
		require.Equal(t, "163-"+source+"-0.1x", marketplaceDisplayName(source, 0.1, "163"))
	}
}

func TestCustomSourceDeclarationSurvivesUpdateAndReconciliation(t *testing.T) {
	label := "自营 Pro 号池 🚀"
	channel := &marketplaceschema.Channel{
		ID: "163", ProviderType: "codex", SubmittedSourceLabel: "Codex Plus",
		ApprovedSourceLabel: "Codex Plus", SourceLabelStatus: marketplacedomain.SourceLabelApproved,
		Status: marketplacedomain.LifecycleActive,
	}
	group := newMarketplaceGroup(channel.ID, 42, "", channel.SubmittedSourceLabel, 0.1, "public")
	group.VerificationStatus = marketplacedomain.VerificationPassed
	group.LifecycleStatus = marketplacedomain.LifecycleActive
	reverify, err := applyChannelUpdate(channel, group, UpdateChannelRequest{SourceLabel: &label})
	require.NoError(t, err)
	require.False(t, reverify)
	require.Equal(t, label, channel.SubmittedSourceLabel)
	require.Equal(t, marketplacedomain.SourceLabelDeclared, channel.SourceLabelStatus)
	require.Equal(t, label, publicSourceLabel(*channel))
	require.Equal(t, "163-"+label+"-0.1x", group.SystemDisplayName)
	require.Equal(t, marketplacedomain.LifecycleActive, group.LifecycleStatus)
	require.Equal(t, marketplacedomain.VerificationPassed, group.VerificationStatus)
	internalName := group.InternalGroupName
	require.NotContains(t, internalName, label)
	normalizeInternalGroupName(group, channel.ID, reconciledSourceLabel(channel.ProviderType, channel.SubmittedSourceLabel))
	require.Equal(t, internalName, group.InternalGroupName)
	require.Equal(t, "163-"+label+"-0.1x", group.SystemDisplayName)
}
