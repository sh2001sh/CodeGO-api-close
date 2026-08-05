package runtime

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFaultDomainConcurrencyShrinksAfterTransientFailures(t *testing.T) {
	domain := "test-concurrency-domain"
	model := "gpt-test-concurrency"
	for i := 0; i < faultDomainConcurrencyFailureThreshold; i++ {
		release, acquired, snapshot := TryAcquireFaultDomainSlot(domain, model)
		require.True(t, acquired)
		require.Equal(t, faultDomainInitialConcurrency, snapshot.Limit)
		release(false, 504)
	}

	release, acquired, snapshot := TryAcquireFaultDomainSlot(domain, model)
	require.True(t, acquired)
	require.Equal(t, faultDomainInitialConcurrency/2, snapshot.Limit)
	release(true, 0)
}

func TestFaultDomainExclusionIsRequestScoped(t *testing.T) {
	c := &gin.Context{}
	domain := "1:shared.example"

	require.False(t, IsFaultDomainExcluded(c, domain))
	ExcludeFaultDomain(c, domain)
	require.True(t, IsFaultDomainExcluded(c, domain))
	require.False(t, IsFaultDomainExcluded(c, "1:other.example"))
}
