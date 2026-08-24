package app

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTokenGroupDefaultsToAuto(t *testing.T) {
	assert.Equal(t, AutoGroupName, NormalizeTokenGroup(""))
	assert.Equal(t, AutoGroupName, NormalizeTokenGroup("  "))
	assert.Equal(t, "premium", NormalizeTokenGroup(" premium "))
}

func TestEffectiveRetryTimesProvidesOneAutomaticRetry(t *testing.T) {
	original := platformconfig.RetryTimes
	t.Cleanup(func() { platformconfig.RetryTimes = original })

	platformconfig.RetryTimes = 0
	assert.Equal(t, 1, EffectiveRetryTimes(AutoGroupName))
	assert.Equal(t, 1, EffectiveRetryTimes("default"))
	assert.Equal(t, 0, EffectiveRetryTimes(""))

	platformconfig.RetryTimes = 2
	assert.Equal(t, 2, EffectiveRetryTimes(AutoGroupName))
}

func TestGetHealthySatisfiedChannelFallsBackAfterPrimaryCooldown(t *testing.T) {
	const modelName = "gpt-route-fallback-test"
	primaryPriority := int64(3)
	fallbackPriority := int64(2)
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() {
		selectRandomSatisfiedChannel = originalSelector
		gatewayruntime.RecordChannelSuccess(42, modelName, 0)
	})

	var retries []int
	selectRandomSatisfiedChannel = func(_ string, _ string, retry int) (*gatewayschema.Channel, error) {
		retries = append(retries, retry)
		if retry == 0 {
			return &gatewayschema.Channel{Id: 42, Priority: &primaryPriority}, nil
		}
		return &gatewayschema.Channel{Id: 39, Priority: &fallbackPriority}, nil
	}
	gatewayruntime.CoolChannelModelForUpstreamFailure(42, modelName)

	channel, err := getHealthySatisfiedChannel("default", modelName, 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 39, channel.Id)
	require.Contains(t, retries, 1)
}

func TestGetHealthySatisfiedChannelUsesLastResortWhenEveryRouteCools(t *testing.T) {
	const modelName = "gpt-route-last-resort-test"
	priority := int64(3)
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() {
		selectRandomSatisfiedChannel = originalSelector
		gatewayruntime.RecordChannelSuccess(42, modelName, 0)
	})

	selectRandomSatisfiedChannel = func(_ string, _ string, _ int) (*gatewayschema.Channel, error) {
		return &gatewayschema.Channel{Id: 42, Priority: &priority}, nil
	}
	gatewayruntime.CoolChannelModelForUpstreamFailure(42, modelName)

	channel, err := getHealthySatisfiedChannel("default", modelName, 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 42, channel.Id)
}

func TestLegacyLastResortKeepsModelAvailableWhenProbeLeaseIsBusy(t *testing.T) {
	const modelName = "gpt-route-busy-probe-fallback-test"
	priority := int64(1)
	channelID := 9_876_545
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() {
		selectRandomSatisfiedChannel = originalSelector
		gatewayruntime.RecordChannelSuccess(channelID, modelName, 0)
	})

	selectRandomSatisfiedChannel = func(_ string, _ string, _ int) (*gatewayschema.Channel, error) {
		return &gatewayschema.Channel{Id: channelID, Priority: &priority}, nil
	}
	for range 3 {
		gatewayruntime.RecordChannelRetryableFailureWithCooldown(channelID, modelName, time.Minute)
	}

	requestType := gatewayruntime.RequestTypeFromContext(nil)
	require.True(t, gatewayruntime.TryStartChannelLastResortProbe(channelID, modelName, requestType))
	leases := make([]*gin.Context, 0, 3)
	for range 3 {
		lease, _ := gin.CreateTestContext(httptest.NewRecorder())
		require.True(t, gatewayruntime.AcquireAllCoolingFallback(lease, "default", modelName, requestType))
		leases = append(leases, lease)
	}
	t.Cleanup(func() {
		for _, lease := range leases {
			gatewayruntime.ReleaseAllCoolingFallbacks(lease)
		}
	})

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	channel, err := getHealthySatisfiedChannelWithContext(context, "default", modelName, 0)
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, channelID, channel.Id)
}

func TestAutoSelectionChecksHealthyFallbackBeforeLastResort(t *testing.T) {
	const modelName = "gpt-auto-last-resort-order-test"
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalSelector := selectRandomSatisfiedChannel
	priority := int64(3)
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		selectRandomSatisfiedChannel = originalSelector
		gatewayruntime.RecordChannelSuccess(42, modelName, 0)
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["low","fallback"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低价","fallback":"备用"}`))
	selectRandomSatisfiedChannel = func(group string, _ string, _ int) (*gatewayschema.Channel, error) {
		if group == "low" {
			return &gatewayschema.Channel{Id: 42, Priority: &priority}, nil
		}
		return &gatewayschema.Channel{Id: 39, Priority: &priority}, nil
	}
	gatewayruntime.CoolChannelModelForUpstreamFailure(42, modelName)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        context,
		TokenGroup: AutoGroupName,
		ModelName:  modelName,
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 39, channel.Id)
	require.Equal(t, "fallback", group)
}

func TestAutoRetryMovesToNextCheapestGroup(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalSelector := selectRandomSatisfiedChannel
	originalSelectableRoute := hasSelectableAutoGroupRoute
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		selectRandomSatisfiedChannel = originalSelector
		hasSelectableAutoGroupRoute = originalSelectableRoute
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["low","fallback"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低价","fallback":"备用"}`))
	calledGroups := make([]string, 0, 2)
	calledRetries := make([]int, 0, 2)
	priority := int64(1)
	selectRandomSatisfiedChannel = func(group string, _ string, retry int) (*gatewayschema.Channel, error) {
		calledGroups = append(calledGroups, group)
		calledRetries = append(calledRetries, retry)
		if group == "low" {
			return &gatewayschema.Channel{Id: 38, Priority: &priority}, nil
		}
		return &gatewayschema.Channel{Id: 39, Priority: &priority}, nil
	}
	hasSelectableAutoGroupRoute = func(group, _ string) (bool, error) {
		return group == "fallback", nil
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	platformhttpctx.SetContextKey(context, constant.ContextKeyUserId, 909)
	retry := 0
	firstChannel, firstGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        context,
		TokenGroup: AutoGroupName,
		ModelName:  "gpt-auto-retry-next-group",
		Retry:      &retry,
	})
	require.NoError(t, err)
	require.NotNil(t, firstChannel)
	require.Equal(t, 38, firstChannel.Id)
	require.Equal(t, "low", firstGroup)
	require.True(t, gatewayruntime.HasRemainingCrossGroupRoute(context))
	for failure := 0; failure < autoGroupFailureThreshold; failure++ {
		context.Set(constant.RequestIdKey, "dynamic-reorder-"+string(rune('a'+failure)))
		recordAutoGroupFailure(context, "low", "gpt-auto-retry-next-group", time.Now())
	}

	retry = 1
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        context,
		TokenGroup: AutoGroupName,
		ModelName:  "gpt-auto-retry-next-group",
		Retry:      &retry,
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 39, channel.Id)
	require.Equal(t, "fallback", group)
	require.Equal(t, []string{"low", "fallback"}, calledGroups)
	require.Equal(t, []int{0, 0}, calledRetries)
	require.False(t, gatewayruntime.HasRemainingCrossGroupRoute(context))
}

func TestAutoSelectionPreservesRetryAfterCandidatesAreExhausted(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		selectRandomSatisfiedChannel = originalSelector
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["low","fallback"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低价","fallback":"备用"}`))
	selectRandomSatisfiedChannel = func(_ string, _ string, _ int) (*gatewayschema.Channel, error) {
		return nil, nil
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	retry := 1
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        context,
		TokenGroup: AutoGroupName,
		ModelName:  "gpt-auto-retry-preserve-after-exhaustion",
		Retry:      &retry,
	})

	require.NoError(t, err)
	assert.Nil(t, channel)
	assert.Equal(t, AutoGroupName, group)
	assert.Equal(t, 1, retry, "candidate exhaustion must not erase the outer retry budget")
}

func TestAutoSelectionIgnoresLaterGroupsWithoutModelRoutes(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalSelector := selectRandomSatisfiedChannel
	originalSelectableRoute := hasSelectableAutoGroupRoute
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		selectRandomSatisfiedChannel = originalSelector
		hasSelectableAutoGroupRoute = originalSelectableRoute
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["plus","claude-only"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"plus":"Plus","claude-only":"Claude"}`))
	priority := int64(1)
	selectRandomSatisfiedChannel = func(group string, _ string, _ int) (*gatewayschema.Channel, error) {
		if group == "plus" {
			return &gatewayschema.Channel{Id: 6, Priority: &priority}, nil
		}
		return nil, nil
	}
	hasSelectableAutoGroupRoute = func(string, string) (bool, error) { return false, nil }

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: context, TokenGroup: AutoGroupName, ModelName: "gpt-5.6-sol", Retry: new(int),
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, "plus", group)
	require.False(t, gatewayruntime.HasRemainingCrossGroupRoute(context))
}

func TestCacheSelectionSkipsCoolingChannelEvenWithLegacyProbeContext(t *testing.T) {
	const modelName = "gpt-route-no-user-probe-test"
	primaryPriority := int64(3)
	fallbackPriority := int64(2)
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() {
		selectRandomSatisfiedChannel = originalSelector
		gatewayruntime.RecordChannelSuccess(42, modelName, 0)
	})

	selectRandomSatisfiedChannel = func(_ string, _ string, retry int) (*gatewayschema.Channel, error) {
		if retry == 0 {
			return &gatewayschema.Channel{Id: 42, Priority: &primaryPriority}, nil
		}
		return &gatewayschema.Channel{Id: 39, Priority: &fallbackPriority}, nil
	}
	gatewayruntime.CoolChannelModelForUpstreamFailure(42, modelName)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("model_probe_channel_id", 42)
	context.Set("model_probe_group", "default")
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        context,
		TokenGroup: "default",
		ModelName:  modelName,
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, 39, channel.Id)
}

func TestOrderAutoGroupsPreservesConfiguredOrderUntilUserCooldown(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalRatios := gatewaystore.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, resetAutoGroupCircuitCacheForTest())
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["high","low"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低费率","high":"高费率"}`))
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(`{"low":0.8,"high":1.2}`))

	baseTime := time.Now()
	context := newAutoGroupTestContext(101, "request-0")
	assert.Equal(t, []string{"high", "low"}, orderGroupsByAutoPolicy(context, "default", "gpt-test", GetUserAutoGroup("default"), baseTime))
	for range autoGroupFailureThreshold {
		requestID := context.GetString(constant.RequestIdKey)
		context.Set(constant.RequestIdKey, requestID+"-next")
		recordAutoGroupFailure(context, "high", "gpt-test", baseTime)
	}
	assert.Equal(t, []string{"low", "high"}, orderGroupsByAutoPolicy(context, "default", "gpt-test", GetUserAutoGroup("default"), baseTime))

	otherUser := newAutoGroupTestContext(202, "request-other")
	assert.Equal(t, []string{"high", "low"}, orderGroupsByAutoPolicy(otherUser, "default", "gpt-test", GetUserAutoGroup("default"), baseTime))
}

func TestOrderAutoGroupsKeepsConfiguredLastResortWhenAllUserGroupsCool(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalSelector := selectRandomSatisfiedChannel
	originalSelectableRoute := hasSelectableAutoGroupRoute
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		selectRandomSatisfiedChannel = originalSelector
		hasSelectableAutoGroupRoute = originalSelectableRoute
		require.NoError(t, resetAutoGroupCircuitCacheForTest())
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["low","fallback"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低费率","fallback":"备用"}`))
	context := newAutoGroupTestContext(303, "request")
	baseTime := time.Now()
	for _, group := range []string{"low", "fallback"} {
		for failure := 0; failure < autoGroupFailureThreshold; failure++ {
			context.Set(constant.RequestIdKey, group+string(rune('a'+failure)))
			recordAutoGroupFailure(context, group, "gpt-all-cooling", baseTime)
		}
	}

	assert.Equal(t, []string{"low", "fallback"}, orderGroupsByAutoPolicy(
		context, "default", "gpt-all-cooling", GetUserAutoGroup("default"), baseTime,
	))
	priority := int64(1)
	selectRandomSatisfiedChannel = func(group, _ string, _ int) (*gatewayschema.Channel, error) {
		if group == "low" {
			return &gatewayschema.Channel{Id: 601, Priority: &priority}, nil
		}
		return &gatewayschema.Channel{Id: 602, Priority: &priority}, nil
	}
	hasSelectableAutoGroupRoute = func(group, _ string) (bool, error) {
		return group == "fallback", nil
	}
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: context, TokenGroup: AutoGroupName, ModelName: "gpt-all-cooling", Retry: new(int),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 601, channel.Id)
	assert.Equal(t, "low", group)
}

func TestAutoGroupRecoveryRequiresTwoSuccessfulUserProbes(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, resetAutoGroupCircuitCacheForTest())
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["low","fallback"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低费率","fallback":"备用"}`))
	context := newAutoGroupTestContext(404, "request")
	baseTime := time.Now()
	for failure := 0; failure < autoGroupFailureThreshold; failure++ {
		context.Set(constant.RequestIdKey, "failure-"+string(rune('a'+failure)))
		recordAutoGroupFailure(context, "low", "gpt-recovery", baseTime)
	}
	assert.Equal(t, []string{"fallback", "low"}, orderGroupsByAutoPolicy(
		context, "default", "gpt-recovery", GetUserAutoGroup("default"), baseTime,
	))

	firstProbeAt := baseTime.Add(autoGroupCooldownBase + time.Second)
	assert.Equal(t, []string{"low", "fallback"}, orderGroupsByAutoPolicy(
		context, "default", "gpt-recovery", GetUserAutoGroup("default"), firstProbeAt,
	))
	recordAutoGroupSuccess(context, "low", "gpt-recovery", firstProbeAt)
	state, found := getAutoGroupCircuit(context, "low", "gpt-recovery")
	require.True(t, found)
	assert.Equal(t, 1, state.RecoveryProbeSuccesses)

	secondProbeAt := firstProbeAt.Add(time.Second)
	assert.Equal(t, []string{"low", "fallback"}, orderGroupsByAutoPolicy(
		context, "default", "gpt-recovery", GetUserAutoGroup("default"), secondProbeAt,
	))
	recordAutoGroupSuccess(context, "low", "gpt-recovery", secondProbeAt)
	_, found = getAutoGroupCircuit(context, "low", "gpt-recovery")
	assert.False(t, found)
	assert.Equal(t, []string{"low", "fallback"}, orderGroupsByAutoPolicy(
		context, "default", "gpt-recovery", GetUserAutoGroup("default"), secondProbeAt,
	))
}

func TestAutoGroupCooldownDoesNotCrossRequestTypes(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, resetAutoGroupCircuitCacheForTest()) })
	shortContext := newAutoGroupTestContext(505, "short")
	gatewayruntime.InitializeRequestProfile(shortContext, "gpt-test", "/v1/responses", gatewayruntime.RequestProfileHint{IsStream: true})
	longContext := newAutoGroupTestContext(505, "long")
	gatewayruntime.InitializeRequestProfile(longContext, "gpt-test", "/v1/responses", gatewayruntime.RequestProfileHint{IsStream: true, HasCacheAffinity: true})
	baseTime := time.Now()
	for failure := 0; failure < autoGroupFailureThreshold; failure++ {
		shortContext.Set(constant.RequestIdKey, "short-"+string(rune('a'+failure)))
		recordAutoGroupFailure(shortContext, "low", "gpt-test", baseTime)
	}

	assert.True(t, isAutoGroupCooling(shortContext, "low", "gpt-test", baseTime))
	assert.False(t, isAutoGroupCooling(longContext, "low", "gpt-test", baseTime))
}

func TestAutoGroupRecoveryProbeLeaseAllowsOneConcurrentRequest(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, resetAutoGroupCircuitCacheForTest()) })
	baseTime := time.Now()
	seed := newAutoGroupTestContext(606, "seed")
	for failure := 0; failure < autoGroupFailureThreshold; failure++ {
		seed.Set(constant.RequestIdKey, "failure-"+string(rune('a'+failure)))
		recordAutoGroupFailure(seed, "low", "gpt-probe-lease", baseTime)
	}
	probeAt := baseTime.Add(autoGroupCooldownBase + time.Second)

	var started atomic.Int32
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	var probes sync.WaitGroup
	for request := 0; request < 2; request++ {
		probes.Add(1)
		go func(request int) {
			defer probes.Done()
			context := newAutoGroupTestContext(606, "probe-"+string(rune('a'+request)))
			ready.Done()
			<-start
			if tryStartAutoGroupRecoveryProbe(context, "low", "gpt-probe-lease", probeAt) {
				started.Add(1)
			}
		}(request)
	}
	ready.Wait()
	close(start)
	probes.Wait()

	assert.Equal(t, int32(1), started.Load())
}

func TestPendingFirstByteDemotesOnlyAffectedUserGroup(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, resetAutoGroupCircuitCacheForTest()) })
	baseTime := time.Now()
	first := newAutoGroupTestContext(707, "pending-a")
	second := newAutoGroupTestContext(707, "pending-b")
	beginAutoGroupAttemptAt(first, "low", "gpt-pending", baseTime)
	beginAutoGroupAttemptAt(second, "low", "gpt-pending", baseTime)

	groups := []string{"low", "fallback"}
	stalledAt := baseTime.Add(autoGroupPendingShortFirstByteWait)
	assert.Equal(t, []string{"fallback", "low"}, orderGroupsByAutoPolicy(
		first, "default", "gpt-pending", append([]string(nil), groups...), stalledAt,
	))
	otherUser := newAutoGroupTestContext(708, "other")
	assert.Equal(t, groups, orderGroupsByAutoPolicy(
		otherUser, "default", "gpt-pending", append([]string(nil), groups...), stalledAt,
	))

	endAutoGroupAttemptAt(first, stalledAt)
	assert.Equal(t, groups, orderGroupsByAutoPolicy(
		second, "default", "gpt-pending", append([]string(nil), groups...), stalledAt,
	))
	endAutoGroupAttemptAt(second, stalledAt)
}

func TestPendingFirstByteUsesLongContextThreshold(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, resetAutoGroupCircuitCacheForTest()) })
	baseTime := time.Now()
	first := newAutoGroupTestContext(808, "pending-long-a")
	second := newAutoGroupTestContext(808, "pending-long-b")
	for _, context := range []*gin.Context{first, second} {
		gatewayruntime.InitializeRequestProfile(context, "gpt-long", "/v1/responses", gatewayruntime.RequestProfileHint{
			IsStream: true, HasCacheAffinity: true,
		})
		beginAutoGroupAttemptAt(context, "low", "gpt-long", baseTime)
	}

	groups := []string{"low", "fallback"}
	assert.Equal(t, groups, orderGroupsByAutoPolicy(
		first, "default", "gpt-long", append([]string(nil), groups...), baseTime.Add(autoGroupPendingShortFirstByteWait),
	))
	assert.Equal(t, []string{"fallback", "low"}, orderGroupsByAutoPolicy(
		first, "default", "gpt-long", append([]string(nil), groups...), baseTime.Add(autoGroupPendingLongFirstByteWait),
	))
}

func TestAutoGroupSuccessDecaysUserFailuresInsteadOfClearingHistory(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, resetAutoGroupCircuitCacheForTest()) })
	context := newAutoGroupTestContext(818, "failure-a")
	baseTime := time.Now()
	recordAutoGroupFailure(context, "low", "gpt-decay", baseTime)
	context.Set(constant.RequestIdKey, "failure-b")
	recordAutoGroupFailure(context, "low", "gpt-decay", baseTime)

	recordAutoGroupSuccess(context, "low", "gpt-decay", baseTime.Add(time.Second))
	state, found := getAutoGroupCircuit(context, "low", "gpt-decay")
	require.True(t, found)
	assert.Equal(t, 1, state.ConsecutiveFailures)
}

func TestAutoGroupFailureWithoutUserDoesNotCreateSharedCooldown(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, resetAutoGroupCircuitCacheForTest()) })
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	baseTime := time.Now()
	for failure := 0; failure < autoGroupFailureThreshold; failure++ {
		context.Set(constant.RequestIdKey, "anonymous-"+string(rune('a'+failure)))
		recordAutoGroupFailure(context, "low", "gpt-anonymous", baseTime)
	}

	assert.False(t, isAutoGroupCooling(context, "low", "gpt-anonymous", baseTime))
}

func newAutoGroupTestContext(userID int, requestID string) *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	platformhttpctx.SetContextKey(context, constant.ContextKeyUserId, userID)
	context.Set(constant.RequestIdKey, requestID)
	return context
}

func TestAutoSelectionFallsBackToPermittedModelSpecificGroup(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		selectRandomSatisfiedChannel = originalSelector
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["default","free"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"default":"默认","free":"免费","claude":"Claude"}`))
	priority := int64(1)
	selectRandomSatisfiedChannel = func(group string, model string, _ int) (*gatewayschema.Channel, error) {
		if group == "claude" && model == "claude-test" {
			return &gatewayschema.Channel{Id: 99, Priority: &priority}, nil
		}
		return nil, nil
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        context,
		TokenGroup: AutoGroupName,
		ModelName:  "claude-test",
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 99, channel.Id)
	assert.Equal(t, "claude", group)
}

func TestOfficialChannelSelectionSkipsExternalCandidate(t *testing.T) {
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() { selectRandomSatisfiedChannel = originalSelector })
	priority := int64(1)
	calls := 0
	selectRandomSatisfiedChannel = func(_ string, _ string, _ int) (*gatewayschema.Channel, error) {
		calls++
		if calls == 1 {
			return &gatewayschema.Channel{Id: 71, Priority: &priority, ChannelScope: gatewayschema.ChannelScopeExternal}, nil
		}
		return &gatewayschema.Channel{Id: 72, Priority: &priority, ChannelScope: gatewayschema.ChannelScopeOfficial}, nil
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(string(constant.ContextKeyOfficialChannelOnly), true)
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: context, TokenGroup: "default", ModelName: "gpt-official-only",
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 72, channel.Id)
	require.Equal(t, "default", group)
}

func TestOfficialChannelSelectionFallsBackWhenNoOfficialCandidateExists(t *testing.T) {
	originalSelector := selectRandomSatisfiedChannel
	t.Cleanup(func() { selectRandomSatisfiedChannel = originalSelector })
	priority := int64(1)
	selectRandomSatisfiedChannel = func(_ string, _ string, _ int) (*gatewayschema.Channel, error) {
		return &gatewayschema.Channel{Id: 73, Priority: &priority, ChannelScope: gatewayschema.ChannelScopeExternal}, nil
	}

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set(string(constant.ContextKeyOfficialChannelOnly), true)
	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: context, TokenGroup: "default", ModelName: "gpt-official-fallback",
	})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 73, channel.Id)
	require.False(t, context.GetBool(string(constant.ContextKeyOfficialChannelOnly)))
	require.True(t, context.GetBool(string(constant.ContextKeyOfficialChannelFallback)))
}
