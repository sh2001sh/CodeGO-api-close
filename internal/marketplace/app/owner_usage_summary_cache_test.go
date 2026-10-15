package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnerUsageSummaryCacheKeyIgnoresPageAndNormalizesIDOrder(t *testing.T) {
	base := OwnerUsageLogQuery{
		Page: 1, PageSize: 20, StartTimestamp: 100, EndTimestamp: 200,
		Status: "success", Search: "request", userFilterIDs: []int{3, 1}, searchUserIDs: []int{9, 7},
	}
	nextPage := base
	nextPage.Page = 53
	nextPage.PageSize = 100
	nextPage.userFilterIDs = []int{1, 3}
	nextPage.searchUserIDs = []int{7, 9}

	require.Equal(
		t,
		ownerUsageSummaryCacheKey(42, []int{2, 1}, []string{"b", "a"}, base),
		ownerUsageSummaryCacheKey(42, []int{1, 2}, []string{"a", "b"}, nextPage),
	)
}
