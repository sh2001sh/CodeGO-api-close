package app

import (
	"encoding/json"
	"testing"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func TestPublicSourceLabelRequiresApproval(t *testing.T) {
	channel := marketplaceschema.Channel{
		SubmittedSourceLabel: "Claude Max",
		ApprovedSourceLabel:  "Claude Max",
		SourceLabelStatus:    marketplacedomain.SourceLabelPending,
	}
	require.Empty(t, publicSourceLabel(channel))

	channel.SourceLabelStatus = marketplacedomain.SourceLabelApproved
	require.Equal(t, "Claude Max", publicSourceLabel(channel))
}

func TestRemoteCompactionSupportSummarizesOnlySupportedVersions(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "v1 only", raw: `{"remote_compaction_v1":{"status":"supported"}}`, want: "v1"},
		{name: "both", raw: `{"remote_compaction_v1":{"status":"supported"},"remote_compaction_v2":{"status":"supported"}}`, want: "v1_v2"},
		{name: "v2 only", raw: `{"remote_compaction_v2":{"status":"supported"}}`, want: "v2"},
		{name: "not ready", raw: `{"remote_compaction_v1":{"status":"error"},"remote_compaction_v2":{"status":"pending"}}`, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, remoteCompactionSupport(test.raw))
		})
	}
}

func TestGroupListItemIncludesRemoteCompactionSupport(t *testing.T) {
	channel := marketplaceschema.Channel{
		ID:                    "channel-compaction",
		TransportCapabilities: `{"remote_compaction_v1":{"status":"supported"},"remote_compaction_v2":{"status":"supported"}}`,
	}
	item := groupListItem(
		marketplaceschema.Group{ID: "group-compaction", ChannelID: channel.ID},
		channel, nil, marketplaceschema.RankingSnapshot{}, nil,
	)
	require.Equal(t, "v1_v2", item.RemoteCompactionSupport)
}

func TestPendingSourceLabelIsNotSearchableOrReturned(t *testing.T) {
	group := marketplaceschema.Group{
		ID: "group-1", ChannelID: "channel-1", SystemDisplayName: "用户分组",
		OwnerDisplayName: "渠道主", PublicSlug: "mg_group1",
		LifecycleStatus: marketplacedomain.LifecyclePendingReview,
		Visibility:      marketplacedomain.VisibilityPublic,
	}
	channel := marketplaceschema.Channel{
		ID: "channel-1", SubmittedSourceLabel: "Kiro", ApprovedSourceLabel: "Kiro",
		SourceLabelStatus: marketplacedomain.SourceLabelPending,
	}
	require.False(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "Kiro"}))
	require.False(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "渠道主"}))
	item := groupListItem(group, channel, nil, marketplaceschema.RankingSnapshot{}, nil)
	require.Empty(t, item.SourceLabel)
	require.Equal(t, "channel-1-来源待审核-0x", item.SystemDisplayName)
}

func TestMarketplaceGroupFiltersByNumericChannelIDModelSourceAndProvider(t *testing.T) {
	group := marketplaceschema.Group{ID: "group-filter", ChannelID: "123456789012", SystemDisplayName: "Codex Plus"}
	channel := marketplaceschema.Channel{
		ID: "123456789012", ProviderType: "openai_compatible",
		ApprovedSourceLabel: "Codex Plus", SourceLabelStatus: marketplacedomain.SourceLabelApproved,
	}
	models := []string{"gpt-5.2-codex", "gpt-4.1"}

	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Search: "123456789012"}))
	require.False(t, matchesGroupQuery(group, channel, models, GroupQuery{Search: "123456"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Search: "group-filter"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Search: "Codex Plus"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Model: "5.2"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Source: "Codex Plus"}))
	require.True(t, matchesGroupQuery(group, channel, models, GroupQuery{Provider: "openai_compatible"}))
	require.False(t, matchesGroupQuery(group, channel, models, GroupQuery{Source: "CC-Kiro"}))
	require.False(t, matchesGroupQuery(group, channel, models, GroupQuery{Provider: "anthropic"}))

	item := groupListItem(group, channel, models, marketplaceschema.RankingSnapshot{}, nil)
	require.Equal(t, channel.ID, item.ChannelID)
	require.Equal(t, channel.ProviderType, item.ProviderType)
	require.Equal(t, "123456789012-Codex Plus-0x", item.SystemDisplayName)

	payload, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "owner_display_name")
	require.NotContains(t, string(payload), "independent_consumers")
}

func TestMarketplaceNumericSearchMatchesExactChannelID(t *testing.T) {
	group := marketplaceschema.Group{
		ID: "group-with-16-in-id", ChannelID: "16",
		SystemDisplayName: "16-Codex Plus-0.09x",
	}
	channel := marketplaceschema.Channel{ID: "16"}

	require.True(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "16"}))

	channel.ID = "25"
	require.False(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "16"}))
	require.True(t, matchesGroupQuery(group, channel, nil, GroupQuery{Search: "group-with"}))
}

func TestFilterGroupsBySourceKeepsOnlyApprovedSource(t *testing.T) {
	groups := []marketplaceschema.Group{
		{ID: "plus-group", ChannelID: "plus-channel"},
		{ID: "pro-group", ChannelID: "pro-channel"},
	}
	channels := map[string]marketplaceschema.Channel{
		"plus-channel": {
			ID: "plus-channel", ApprovedSourceLabel: "Codex Plus",
			SourceLabelStatus: marketplacedomain.SourceLabelApproved,
		},
		"pro-channel": {
			ID: "pro-channel", ApprovedSourceLabel: "Codex Pro",
			SourceLabelStatus: marketplacedomain.SourceLabelApproved,
		},
	}

	filtered, filteredChannels := filterGroupsBySource(groups, channels, "codex plus")
	require.Equal(t, []string{"plus-group"}, []string{filtered[0].ID})
	require.Len(t, filteredChannels, 1)
	require.Contains(t, filteredChannels, "plus-channel")
}

func TestGroupListItemUsesLatestModelVerificationTime(t *testing.T) {
	t.Parallel()
	earlier := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	later := earlier.Add(5 * time.Minute)
	encoded := encodeModelVerificationResults([]ModelVerificationResult{
		{Model: "gpt-a", Status: "passed", TestedAt: earlier},
		{Model: "gpt-b", Status: "passed", TestedAt: later},
	})

	item := groupListItem(
		marketplaceschema.Group{ID: "group-time", ChannelID: "channel-time"},
		marketplaceschema.Channel{ID: "channel-time", ModelVerificationResults: encoded},
		nil,
		marketplaceschema.RankingSnapshot{IndependentConsumers: 7},
		nil,
	)

	require.NotNil(t, item.VerificationCompletedAt)
	require.Equal(t, later, *item.VerificationCompletedAt)
	payload, err := json.Marshal(item)
	require.NoError(t, err)
	require.Contains(t, string(payload), "verification_completed_at")
	require.NotContains(t, string(payload), "independent_consumers")
}

func TestFilterAndSortGroupsMapsCurrentConcurrencyByInternalChannel(t *testing.T) {
	originalRedisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	t.Cleanup(func() { platformcache.RedisEnabled = originalRedisEnabled })

	group := marketplaceschema.Group{ID: "group-concurrency", ChannelID: "channel-concurrency"}
	internalChannelID := 808
	channel := marketplaceschema.Channel{
		ID:                 "channel-concurrency",
		InternalChannelID:  &internalChannelID,
		MaxConcurrency:     12,
		UserMaxConcurrency: 3,
	}
	release, admitted := gatewayruntime.TryBeginChannelRequest(internalChannelID, channel.MaxConcurrency)
	require.True(t, admitted)
	defer release()

	items := filterAndSortGroups(
		[]marketplaceschema.Group{group},
		map[string]marketplaceschema.Channel{channel.ID: channel},
		map[string]marketplaceschema.RankingSnapshot{},
		nil,
		GroupQuery{},
	)

	require.Len(t, items, 1)
	require.Equal(t, 12, items[0].MaxConcurrency)
	require.Equal(t, 3, items[0].UserMaxConcurrency)
	require.Equal(t, 1, items[0].CurrentConcurrency)
}

func TestFilterAndSortGroupsUsesAdmissionLeasesForLimitedChannels(t *testing.T) {
	originalLoader := loadActiveChannelRequestLeases
	loadActiveChannelRequestLeases = func(channelIDs []int) (map[int]int, bool) {
		require.Equal(t, []int{818}, channelIDs)
		return map[int]int{818: 7}, true
	}
	t.Cleanup(func() { loadActiveChannelRequestLeases = originalLoader })

	internalChannelID := 818
	channel := marketplaceschema.Channel{
		ID:                "channel-leases",
		InternalChannelID: &internalChannelID,
		MaxConcurrency:    10,
	}
	items := filterAndSortGroups(
		[]marketplaceschema.Group{{ID: "group-leases", ChannelID: channel.ID}},
		map[string]marketplaceschema.Channel{channel.ID: channel},
		map[string]marketplaceschema.RankingSnapshot{},
		nil,
		GroupQuery{},
	)

	require.Len(t, items, 1)
	require.Equal(t, 7, items[0].CurrentConcurrency)
}

func TestGroupListItemReturnsSanitizedGPT56MappingReport(t *testing.T) {
	t.Parallel()
	checkedAt := time.Date(2026, 8, 17, 9, 30, 0, 0, time.UTC)
	encoded, err := json.Marshal([]GPT56MappingResult{{
		RequestedModel: "gpt-5.6-terra",
		Status:         GPT56MappingStatusInsufficientEvidence,
		SampleCount:    3,
		MatchedSamples: 2,
		Error:          "raw secret upstream error",
		Samples: []GPT56MappingSample{{
			Index: 3, Status: GPT56MappingSampleStatusError,
			Error: "raw secret upstream error", TestedAt: checkedAt,
		}},
	}})
	require.NoError(t, err)

	item := groupListItem(
		marketplaceschema.Group{ID: "group-report", ChannelID: "channel-report"},
		marketplaceschema.Channel{
			ID: "channel-report", GPT56MappingResults: string(encoded),
			GPT56MappingStatus:    GPT56MappingStatusInsufficientEvidence,
			GPT56MappingCheckedAt: &checkedAt,
		},
		[]string{"gpt-5.6-terra"},
		marketplaceschema.RankingSnapshot{},
		nil,
	)

	require.Equal(t, GPT56MappingStatusInsufficientEvidence, item.GPT56MappingStatus)
	require.Equal(t, checkedAt, *item.GPT56MappingCheckedAt)
	require.Len(t, item.GPT56MappingResults, 1)
	require.Empty(t, item.GPT56MappingResults[0].Error)
	require.Equal(t, "请求失败，未获得可验证结果", item.GPT56MappingResults[0].Samples[0].Error)
	payload, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "raw secret upstream error")
}

func TestPublicPendingReviewGroupIsLoadedForMarketplace(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	channel := marketplaceschema.Channel{
		ID: "channel-pending", OwnerUserID: 10, ProviderType: "anthropic",
		SubmittedSourceLabel: "Claude Max", SourceLabelStatus: marketplacedomain.SourceLabelPending,
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		Status: marketplacedomain.LifecyclePendingReview,
	}
	group := marketplaceschema.Group{
		ID: "group-pending", ChannelID: channel.ID, OwnerUserID: 10,
		PublicSlug: "mg_pending", SystemDisplayName: "待审核公开分组",
		InternalGroupName: "market_pending", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecyclePendingReview,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	groups, channels, err := loadPublicGroups(GroupQuery{})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, group.ID, groups[0].ID)
	require.Empty(t, publicSourceLabel(channels[channel.ID]))
}

func TestPublicDiscoveryExcludesSuspendedGroups(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	channel := marketplaceschema.Channel{
		ID: "channel-suspended", OwnerUserID: 10, ProviderType: "openai",
		BaseURLCiphertext: "encrypted-url", CredentialCiphertext: "encrypted-key",
		Status: marketplacedomain.LifecycleSuspended,
	}
	group := marketplaceschema.Group{
		ID: "group-suspended", ChannelID: channel.ID, OwnerUserID: 10,
		PublicSlug: "mg_suspended", SystemDisplayName: "暂停渠道",
		InternalGroupName: "market_suspended", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus:    marketplacedomain.LifecycleSuspended,
		VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility:         marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)

	groups, _, err := loadPublicGroups(GroupQuery{Status: marketplacedomain.LifecycleSuspended})
	require.NoError(t, err)
	require.Empty(t, groups)
}

func TestLatestRequestStatusUsesMostRecentNonEmptyBucket(t *testing.T) {
	t.Parallel()

	require.Equal(t, "unknown", latestRequestStatus(nil))
	require.Equal(t, "healthy", latestRequestStatus([]RecentRequestBucket{{SuccessRate: 90, RequestCount: 3}}))
	require.Equal(t, "unstable", latestRequestStatus([]RecentRequestBucket{{SuccessRate: 89.99, RequestCount: 8}}))
	require.Equal(t, "unstable", latestRequestStatus([]RecentRequestBucket{{SuccessRate: 85, RequestCount: 8}}))
	require.Equal(t, "failed", latestRequestStatus([]RecentRequestBucket{{SuccessRate: 84.99, RequestCount: 8}}))
	require.Equal(t, "failed", latestRequestStatus([]RecentRequestBucket{
		{SuccessRate: 100, RequestCount: 4},
		{SuccessRate: 70, RequestCount: 2},
		{SuccessRate: 0, RequestCount: 0},
	}))
}

func TestLatestRequestStatusIgnoresNewerProbeState(t *testing.T) {
	t.Parallel()

	require.Equal(t, "healthy", latestRequestStatus([]RecentRequestBucket{{
		SuccessRate:  100,
		RequestCount: 20,
	}}))
}

func TestMarketplaceHighlightsUseAllFilteredItems(t *testing.T) {
	items := []GroupListItem{
		{ID: "page-one", SystemDisplayName: "Page One", Score: 80, Multiplier: 1, AttemptTTFTP50Ms: 500, LatencySampleCount: 10},
		{ID: "global-best", SystemDisplayName: "Global Best", Score: 98, Multiplier: 1.2, AttemptTTFTP50Ms: 400, LatencySampleCount: 10},
		{ID: "global-cheapest", SystemDisplayName: "Global Cheapest", Score: 75, Multiplier: 0.2, AttemptTTFTP50Ms: 300, LatencySampleCount: 10},
		{ID: "global-fastest", SystemDisplayName: "Global Fastest", Score: 70, Multiplier: 0.8, AttemptTTFTP50Ms: 80, LatencySampleCount: 10},
		{ID: "observing", SystemDisplayName: "Observing", Score: 100, Multiplier: 0.1, AttemptTTFTP50Ms: 50, LatencySampleCount: 10, Observing: true},
	}

	highlights := marketplaceHighlights(items)
	require.Equal(t, "global-best", highlights.Best.GroupID)
	require.Equal(t, "observing", highlights.Cheapest.GroupID)
	require.Equal(t, "observing", highlights.Fastest.GroupID)
}
