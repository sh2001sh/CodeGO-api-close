package store

import (
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
	cacheKey := fmt.Sprintf("%d:%d:%d:%s", startTime, endTime, bucketSize, groups[0])
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
