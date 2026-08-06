package app

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	gatewaygroups "github.com/sh2001sh/new-api/internal/gateway/groupsettings"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
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

func TestOrderAutoGroupsPrefersLowerRateUntilCooldown(t *testing.T) {
	originalAutoGroups := gatewaygroups.AutoGroups2JsonString()
	originalUsableGroups := gatewaygroups.UserUsableGroups2JSONString()
	originalRatios := gatewaystore.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, resetAutoGroupCircuitCacheForTest())
	})

	require.NoError(t, gatewaygroups.UpdateAutoGroupsByJsonString(`["low","high"]`))
	require.NoError(t, gatewaygroups.UpdateUserUsableGroupsByJSONString(`{"low":"低费率","high":"高费率"}`))
	require.NoError(t, gatewaystore.UpdateGroupRatioByJSONString(`{"low":0.8,"high":1.2}`))

	assert.Equal(t, []string{"low", "high"}, OrderAutoGroups("default", "gpt-test"))
	for range autoGroupFailureThreshold {
		recordAutoGroupFailure("low", "gpt-test", time.Now())
	}
	assert.Equal(t, []string{"high", "low"}, OrderAutoGroups("default", "gpt-test"))

	recordAutoGroupSuccess("low", "gpt-test")
	assert.Equal(t, []string{"low", "high"}, OrderAutoGroups("default", "gpt-test"))
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
