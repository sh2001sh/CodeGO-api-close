package store

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestGroupStatusCacheUsesStaleWhileRevalidateWindow(t *testing.T) {
	now := time.Now()
	cacheKey := "stale-while-revalidate"
	rows := []GroupModelRequestBucket{{GroupName: "plus", RequestCount: 3}}
	storeGroupStatusCache(cacheKey, rows, now.Add(time.Minute), now.Add(5*time.Minute), now)
	t.Cleanup(func() {
		groupStatusCache.Lock()
		delete(groupStatusCache.items, cacheKey)
		groupStatusCache.Unlock()
	})

	freshRows, freshState := loadGroupStatusCache(cacheKey, now.Add(30*time.Second))
	require.Equal(t, groupStatusCacheFresh, freshState)
	require.Equal(t, rows, freshRows)

	staleRows, staleState := loadGroupStatusCache(cacheKey, now.Add(90*time.Second))
	require.Equal(t, groupStatusCacheStale, staleState)
	require.Equal(t, rows, staleRows)

	expiredRows, expiredState := loadGroupStatusCache(cacheKey, now.Add(6*time.Minute))
	require.Equal(t, groupStatusCacheMiss, expiredState)
	require.Nil(t, expiredRows)
}

func TestGroupStatusCacheMaxAgeIsBounded(t *testing.T) {
	require.Equal(t, 5*time.Minute, groupStatusCacheMaxAge(10*time.Second))
	require.Equal(t, 10*time.Minute, groupStatusCacheMaxAge(2*time.Minute))
	require.Equal(t, 30*time.Minute, groupStatusCacheMaxAge(10*time.Minute))
}

func TestLoadGroupModelRequestBucketsCollapsesConcurrentMisses(t *testing.T) {
	originalLoader := loadGroupModelRequestBuckets
	originalTTL := platformconfig.GroupStatusCacheSeconds
	startTime := time.Now().UnixNano()
	endTime := startTime + 60
	bucketSize := int64(60)
	groups := []string{"plus"}
	cacheKey := fmt.Sprintf("%d:%d:%d", startTime, endTime, bucketSize)
	t.Cleanup(func() {
		loadGroupModelRequestBuckets = originalLoader
		platformconfig.GroupStatusCacheSeconds = originalTTL
		groupStatusCache.Lock()
		delete(groupStatusCache.items, cacheKey)
		groupStatusCache.Unlock()
	})

	var queryCount atomic.Int32
	loadGroupModelRequestBuckets = func(_, _, _ int64, _ []string) ([]GroupModelRequestBucket, error) {
		queryCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		return []GroupModelRequestBucket{{GroupName: "plus", RequestCount: 1}}, nil
	}
	platformconfig.GroupStatusCacheSeconds = 60

	const callers = 16
	start := make(chan struct{})
	errors := make(chan error, callers)
	var waitGroup sync.WaitGroup
	for range callers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := LoadGroupModelRequestBuckets(startTime, endTime, bucketSize, groups)
			errors <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errors)

	for err := range errors {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, queryCount.Load())
}

func TestGroupStatusSharesWindowAcrossVisibilityScopes(t *testing.T) {
	oldLoader := loadGroupModelRequestBuckets
	startTime := time.Now().UnixNano()
	t.Cleanup(func() {
		loadGroupModelRequestBuckets = oldLoader
		groupStatusCache.Lock()
		delete(groupStatusCache.items, fmt.Sprintf("%d:%d:60", startTime, startTime+60))
		groupStatusCache.Unlock()
	})
	var queries atomic.Int32
	loadGroupModelRequestBuckets = func(_, _, _ int64, groups []string) ([]GroupModelRequestBucket, error) {
		queries.Add(1)
		time.Sleep(20 * time.Millisecond)
		if len(groups) > 0 {
			return []GroupModelRequestBucket{{GroupName: groups[0], RequestCount: 2}}, nil
		}
		return []GroupModelRequestBucket{{GroupName: "public", RequestCount: 2}, {GroupName: "private", RequestCount: 3}}, nil
	}
	const callers = 32
	var wg sync.WaitGroup
	results := make(chan []GroupModelRequestBucket, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			group := "public"
			if i%2 == 1 {
				group = "private"
			}
			rows, err := LoadGroupModelRequestBuckets(startTime, startTime+60, 60, []string{group})
			if err != nil || len(rows) != 1 || rows[0].GroupName != group {
				results <- nil
				return
			}
			results <- rows
		}()
	}
	wg.Wait()
	close(results)
	for rows := range results {
		require.Len(t, rows, 1, "shared statistics must not leak another visibility scope")
	}
	require.EqualValues(t, 1, queries.Load())
	unknown, err := LoadGroupModelRequestBuckets(startTime, startTime+60, 60, []string{"missing"})
	require.NoError(t, err)
	require.Empty(t, unknown)
	rows, err := LoadGroupModelRequestBuckets(startTime, startTime+60, 60, []string{"public"})
	require.NoError(t, err)
	rows[0].GroupName = "changed"
	rows, err = LoadGroupModelRequestBuckets(startTime, startTime+60, 60, []string{"public"})
	require.NoError(t, err)
	require.Equal(t, "public", rows[0].GroupName, "callers must not mutate the shared cache")
}

func TestGroupStatusSharedCacheRetriesFailedLoad(t *testing.T) {
	oldLoader := loadGroupModelRequestBuckets
	startTime := time.Now().UnixNano()
	t.Cleanup(func() {
		loadGroupModelRequestBuckets = oldLoader
		groupStatusCache.Lock()
		delete(groupStatusCache.items, fmt.Sprintf("%d:%d:60", startTime, startTime+60))
		groupStatusCache.Unlock()
	})
	loadErr := errors.New("database unavailable")
	calls := 0
	loadGroupModelRequestBuckets = func(_, _, _ int64, groups []string) ([]GroupModelRequestBucket, error) {
		calls++
		if calls == 1 {
			return nil, loadErr
		}
		return []GroupModelRequestBucket{{GroupName: "public"}, {GroupName: "private"}}, nil
	}
	rows, err := LoadGroupModelRequestBuckets(startTime, startTime+60, 60, []string{"public"})
	require.ErrorIs(t, err, loadErr)
	require.Empty(t, rows)
	rows, err = LoadGroupModelRequestBuckets(startTime, startTime+60, 60, []string{"private"})
	require.NoError(t, err)
	require.Equal(t, []GroupModelRequestBucket{{GroupName: "private"}}, rows)
	require.Equal(t, 2, calls)
}
