package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestOwnerChannelIndexRetainsGlobalSearchIncomeAndDeletedHistory(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.Settlement{}))
	var channels []marketplaceschema.Channel
	var groups []marketplaceschema.Group
	for i := 0; i < 121; i++ {
		id := fmt.Sprintf("owner-%03d", i)
		channels = append(channels, marketplaceschema.Channel{
			ID: id, OwnerUserID: 42, ProviderType: "openai_compatible", SubmittedSourceLabel: "官方",
			BaseURLCiphertext: "private-url", CredentialCiphertext: "private-key",
			DeclaredModels: `["model-global-search"]`, ConnectivityTestStatus: "passed",
			ModelVerificationResults: `[{"model":"model-global-search","status":"passed","listed":true,"error":"` + strings.Repeat("x", 8192) + `"}]`,
		})
		group := autoRouteTestGroup("group-"+id, id, 42, 1)
		group.CustomName, group.Description, group.Tags = "自定义名", "搜索备注", `["search-tag"]`
		groups = append(groups, group)
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&groups).Error)
	foreign := marketplaceschema.Channel{ID: "foreign", OwnerUserID: 43, BaseURLCiphertext: "private", CredentialCiphertext: "private"}
	require.NoError(t, db.Create(&foreign).Error)
	foreignGroup := autoRouteTestGroup("group-foreign", foreign.ID, 43, 1)
	require.NoError(t, db.Create(&foreignGroup).Error)
	require.NoError(t, db.Model(&channels[0]).Update("model_verification_results", `[{"model":"model-global-search","status":"passed","listed":false}]`).Error)
	require.NoError(t, db.Delete(&channels[120]).Error)
	require.NoError(t, db.Delete(&groups[120]).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "income-last", RequestID: "income-last", GroupID: groups[120].ID,
		OwnerUserID: 42, ConsumerUserID: 7, OwnerNetAmount: 9876, Status: "pending", AvailableAt: time.Now(),
	}).Error)

	index, err := ListOwnerChannelIndex(42)
	require.NoError(t, err)
	require.Len(t, index, 121)
	var totalIncome int64
	byID := make(map[string]OwnerChannelIndexItem)
	for _, item := range index {
		byID[item.ID] = item
		totalIncome += item.TotalIncome
		require.Contains(t, item.SearchText, "model-global-search")
		require.Contains(t, item.SearchText, "search-tag")
		require.Contains(t, item.SearchText, "搜索备注")
		require.Contains(t, item.SearchText, "自定义名")
	}
	require.EqualValues(t, 9876, totalIncome)
	require.NotNil(t, byID[channels[120].ID].DeletedAt)
	require.True(t, byID[channels[0].ID].NeedsAttention)
	require.False(t, byID[channels[1].ID].NeedsAttention)
	require.NotContains(t, byID, "foreign")
	encodedIndex, err := json.Marshal(index)
	require.NoError(t, err)
	require.NotContains(t, string(encodedIndex), "model_verification_results")
	require.NotContains(t, string(encodedIndex), "private-key")

	legacy, err := ListOwnerChannels(42)
	require.NoError(t, err)
	require.Len(t, legacy, 121)
	encodedLegacy, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.Greater(t, len(encodedLegacy), len(encodedIndex)*5)
	t.Logf("121 channel JSON bytes: legacy=%d index=%d", len(encodedLegacy), len(encodedIndex))
}

func TestOwnerChannelIndexOverlappingReadsKeepOwnerScopeAndFreshness(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// A plain SQLite :memory: database belongs to one connection.
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.Settlement{}))
	for _, owner := range []int{42, 43} {
		channel := marketplaceschema.Channel{ID: fmt.Sprintf("scope-%d", owner), OwnerUserID: owner}
		group := autoRouteTestGroup("group-"+channel.ID, channel.ID, owner, 1)
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&group).Error)
		if owner == 42 {
			require.NoError(t, db.Delete(&channel).Error)
		}
	}
	results := make([][]OwnerChannelIndexItem, 20)
	errors := make([]error, len(results))
	var workers sync.WaitGroup
	start := make(chan struct{})
	for index := range results {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			results[index], errors[index] = ListOwnerChannelIndex(42 + index%2)
		}(index)
	}
	close(start)
	workers.Wait()
	for index := range results {
		require.NoError(t, errors[index])
		require.Len(t, results[index], 1)
		require.Equal(t, fmt.Sprintf("scope-%d", 42+index%2), results[index][0].ID)
	}
	oldDeletedAt := *results[2][0].DeletedAt
	results[0][0].ID = "caller-mutated"
	*results[0][0].DeletedAt = time.Time{}
	require.Equal(t, "scope-42", results[2][0].ID)
	require.Equal(t, oldDeletedAt, *results[2][0].DeletedAt)
	require.NoError(t, db.Model(&marketplaceschema.Group{}).Where("channel_id = ?", "scope-43").Update("custom_name", "fresh-value").Error)
	fresh, err := ListOwnerChannelIndex(43)
	require.NoError(t, err)
	require.Equal(t, "fresh-value", fresh[0].CustomName)
}

func TestOwnerChannelHydrationIsBoundedAndScoped(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &marketplaceschema.Settlement{}, &marketplaceschema.VerificationRun{}))
	for _, owner := range []int{42, 43} {
		channel := marketplaceschema.Channel{
			ID: fmt.Sprintf("owned-%d", owner), OwnerUserID: owner,
			BaseURLCiphertext: "private", CredentialCiphertext: "private",
			DeclaredModels: `["model-a"]`,
			ModelPrices:    `{"model-a":{"billing_mode":"per_call","price":1}}`,
			Status:         marketplacedomain.LifecycleActive,
		}
		group := autoRouteTestGroup("group-"+channel.ID, channel.ID, owner, 1)
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&group).Error)
	}
	items, err := ListOwnerChannelsByIDs(42, []string{"owned-42", "owned-43", "absent"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "owned-42", items[0].ID)
	require.Equal(t, []string{"model-a"}, items[0].DeclaredModels)
	items, err = ListOwnerChannelsByIDs(42, nil)
	require.NoError(t, err)
	require.Empty(t, items)
	_, err = ListOwnerChannelsByIDs(42, make([]string, 51))
	require.ErrorContains(t, err, "50")
	_, err = ListOwnerChannelsByIDs(0, []string{"owned-42"})
	require.Error(t, err)
	_, err = ListOwnerChannelIndex(0)
	require.Error(t, err)
	items, err = ListOwnerChannelsByIDs(42, []string{"owned-42", "owned-42"})
	require.NoError(t, err)
	require.Len(t, items, 1)
}
