package runtime

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func resetAllCoolingFallbacksForTest(t *testing.T) {
	t.Helper()
	allCoolingFallbacks.Lock()
	allCoolingFallbacks.states = make(map[string]*allCoolingFallbackState)
	allCoolingFallbacks.Unlock()
	t.Cleanup(func() {
		allCoolingFallbacks.Lock()
		allCoolingFallbacks.states = make(map[string]*allCoolingFallbackState)
		allCoolingFallbacks.Unlock()
	})
}

func TestAllCoolingFallbackLimitsConcurrentRequestsAndWakesWaiter(t *testing.T) {
	resetAllCoolingFallbacksForTest(t)
	gin.SetMode(gin.TestMode)
	contexts := make([]*gin.Context, 0, allCoolingFallbackSlots)
	for range allCoolingFallbackSlots {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		require.True(t, AcquireAllCoolingFallback(context, "premium", "gpt-test", RequestTypeChatShortStream))
		contexts = append(contexts, context)
	}
	defer func() {
		for _, context := range contexts {
			ReleaseAllCoolingFallbacks(context)
		}
	}()

	waiter, _ := gin.CreateTestContext(httptest.NewRecorder())
	waiter.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	result := make(chan bool, 1)
	go func() {
		result <- AcquireAllCoolingFallback(waiter, "premium", "gpt-test", RequestTypeChatShortStream)
	}()

	select {
	case acquired := <-result:
		t.Fatalf("waiter acquired before a slot was released: %t", acquired)
	case <-time.After(30 * time.Millisecond):
	}
	ReleaseAllCoolingFallbacks(contexts[0])

	select {
	case acquired := <-result:
		require.True(t, acquired)
		ReleaseAllCoolingFallbacks(waiter)
	case <-time.After(allCoolingFallbackWait):
		t.Fatal("waiter did not wake after a slot was released")
	}
}

func TestAllCoolingFallbackStopsWaitingAtBoundedDeadline(t *testing.T) {
	resetAllCoolingFallbacksForTest(t)
	gin.SetMode(gin.TestMode)
	contexts := make([]*gin.Context, 0, allCoolingFallbackSlots)
	for range allCoolingFallbackSlots {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		require.True(t, AcquireAllCoolingFallback(context, "premium", "gpt-timeout", RequestTypeChatShortStream))
		contexts = append(contexts, context)
	}
	defer func() {
		for _, context := range contexts {
			ReleaseAllCoolingFallbacks(context)
		}
	}()

	waiter, _ := gin.CreateTestContext(httptest.NewRecorder())
	waiter.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	started := time.Now()
	require.False(t, AcquireAllCoolingFallback(waiter, "premium", "gpt-timeout", RequestTypeChatShortStream))
	require.GreaterOrEqual(t, time.Since(started), allCoolingFallbackWait-30*time.Millisecond)
}

func TestAllCoolingFallbackHandles150RPMEquivalentArrivalRate(t *testing.T) {
	resetAllCoolingFallbacksForTest(t)
	gin.SetMode(gin.TestMode)
	const requests = 15
	const arrivalInterval = 40 * time.Millisecond
	const upstreamAttempt = 100 * time.Millisecond

	var active atomic.Int32
	var peak atomic.Int32
	var succeeded atomic.Int32
	var waitGroup sync.WaitGroup
	for range requests {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			if !AcquireAllCoolingFallback(context, "premium", "gpt-150rpm", RequestTypeChatShortStream) {
				return
			}
			current := active.Add(1)
			for {
				previous := peak.Load()
				if current <= previous || peak.CompareAndSwap(previous, current) {
					break
				}
			}
			time.Sleep(upstreamAttempt)
			active.Add(-1)
			ReleaseAllCoolingFallbacks(context)
			succeeded.Add(1)
		}()
		time.Sleep(arrivalInterval)
	}
	waitGroup.Wait()

	require.Equal(t, int32(requests), succeeded.Load())
	require.LessOrEqual(t, peak.Load(), int32(allCoolingFallbackSlots))
}
