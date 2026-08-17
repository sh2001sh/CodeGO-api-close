package runtime

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func resetFaultDomainConcurrencyForTest(t *testing.T) {
	t.Helper()
	faultDomainConcurrency.Lock()
	faultDomainConcurrency.states = make(map[string]*faultDomainConcurrencyState)
	faultDomainConcurrency.Unlock()
	t.Cleanup(func() {
		faultDomainConcurrency.Lock()
		faultDomainConcurrency.states = make(map[string]*faultDomainConcurrencyState)
		faultDomainConcurrency.Unlock()
	})
}

func TestFaultDomainConcurrencyIsIsolatedByRequestType(t *testing.T) {
	resetFaultDomainConcurrencyForTest(t)
	domain := "test-request-type-isolation-domain"
	model := "gpt-test-request-type-isolation"
	releases := make([]func(bool, int), 0, faultDomainInitialConcurrency)
	for i := 0; i < faultDomainInitialConcurrency; i++ {
		release, acquired, _ := TryAcquireFaultDomainSlot(domain, model, RequestTypeChatShortStream)
		require.True(t, acquired)
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release(true, 0)
		}
	}()

	_, acquired, _ := TryAcquireFaultDomainSlot(domain, model, RequestTypeChatShortStream)
	require.False(t, acquired)
	otherRelease, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 99, RequestTypeChatShortStream)
	require.True(t, acquired)
	otherRelease(true, 0)
	longRelease, acquired, _ := TryAcquireFaultDomainSlot(domain, model, RequestTypeChatLongStream)
	require.True(t, acquired)
	longRelease(true, 0)
}

func TestFaultDomainConcurrencyShrinksAfterTransientFailures(t *testing.T) {
	resetFaultDomainConcurrencyForTest(t)
	domain := "test-concurrency-domain"
	model := "gpt-test-concurrency"
	for i := 0; i < faultDomainConcurrencyFailureThreshold; i++ {
		release, acquired, snapshot := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 42)
		require.True(t, acquired)
		require.Zero(t, snapshot.Limit)
		require.Equal(t, faultDomainInitialConcurrency, snapshot.UserLimit)
		release(false, 504)
	}

	release, acquired, snapshot := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 42)
	require.True(t, acquired)
	require.Zero(t, snapshot.Limit)
	require.Equal(t, faultDomainInitialConcurrency/2, snapshot.UserLimit)
	release(true, 0)
}

func TestFaultDomainConcurrencyFailureIsolatedByUser(t *testing.T) {
	resetFaultDomainConcurrencyForTest(t)
	domain := "test-user-isolation-domain"
	model := "gpt-test-user-isolation"
	for i := 0; i < faultDomainConcurrencyFailureThreshold; i++ {
		release, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 7)
		require.True(t, acquired)
		release(false, 503)
	}

	_, acquired, snapshot := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 7)
	require.True(t, acquired)
	require.Equal(t, faultDomainInitialConcurrency/2, snapshot.UserLimit)
	otherRelease, acquired, snapshot := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 8)
	require.True(t, acquired)
	require.Equal(t, faultDomainInitialConcurrency, snapshot.UserLimit)
	otherRelease(true, 0)
}

func TestFaultDomainConcurrencyFailureIsolatedByGroup(t *testing.T) {
	resetFaultDomainConcurrencyForTest(t)
	domain := "test-group-isolation-domain"
	model := "gpt-test-group-isolation"
	for range faultDomainConcurrencyFailureThreshold {
		release, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 7)
		require.True(t, acquired)
		release(false, 503)
	}

	groupARelease, acquired, groupA := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 7)
	require.True(t, acquired)
	require.Equal(t, faultDomainInitialConcurrency/2, groupA.UserLimit)
	groupARelease(true, 0)

	groupBRelease, acquired, groupB := TryAcquireFaultDomainSlotForUser(domain, "group-b", model, 7)
	require.True(t, acquired)
	require.Equal(t, faultDomainInitialConcurrency, groupB.UserLimit)
	groupBRelease(true, 0)
}

func TestFaultDomainConcurrencyAdmissionIsolatedByUserAndGroup(t *testing.T) {
	resetFaultDomainConcurrencyForTest(t)
	domain := "test-admission-isolation-domain"
	model := "gpt-test-admission-isolation"
	releases := make([]func(bool, int), 0, faultDomainInitialConcurrency)
	for range faultDomainInitialConcurrency {
		release, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 7)
		require.True(t, acquired)
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release(true, 0)
		}
	}()

	_, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 7)
	require.False(t, acquired)

	otherUserRelease, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-a", model, 8)
	require.True(t, acquired)
	otherUserRelease(true, 0)

	otherGroupRelease, acquired, _ := TryAcquireFaultDomainSlotForUser(domain, "group-b", model, 7)
	require.True(t, acquired)
	otherGroupRelease(true, 0)
}

func TestFaultDomainConcurrencyDoesNotShrinkForStreamClosureOrGatewayTimeout(t *testing.T) {
	resetFaultDomainConcurrencyForTest(t)
	domain := "test-request-local-stream-failure-domain"
	model := "gpt-test-request-local-stream-failure"

	for _, statusCode := range []int{502, 524} {
		for i := 0; i < faultDomainConcurrencyFailureThreshold; i++ {
			release, acquired, snapshot := TryAcquireFaultDomainSlot(domain, model)
			require.True(t, acquired)
			require.Zero(t, snapshot.Limit)
			release(false, statusCode)
		}
	}

	_, acquired, snapshot := TryAcquireFaultDomainSlot(domain, model)
	require.True(t, acquired)
	require.Zero(t, snapshot.Limit)
}

func TestFaultDomainExclusionIsRequestScoped(t *testing.T) {
	c := &gin.Context{}
	domain := "1:shared.example"

	require.False(t, IsFaultDomainExcluded(c, domain))
	ExcludeFaultDomain(c, domain)
	require.True(t, IsFaultDomainExcluded(c, domain))
	require.False(t, IsFaultDomainExcluded(c, "1:other.example"))
}
