package app

import (
	"errors"
	"fmt"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/notifyx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"math"
	"net/http"
	"sync"
	"time"
)

var testAllChannelsLock sync.Mutex
var testAllChannelsRunning bool
var autoTestChannelsOnce sync.Once

// TestChannelByID runs the test flow for one channel and returns the elapsed seconds.
func TestChannelByID(channelID int, testModel string, endpointType string, isStream bool) (float64, *types.NewAPIError, error) {
	channel, err := getChannelForTest(channelID)
	if err != nil {
		return 0, nil, err
	}

	tik := time.Now()
	result := testChannel(channel, testModel, endpointType, isStream)
	if result.localErr != nil {
		return 0, result.newAPIError, result.localErr
	}

	milliseconds := time.Since(tik).Milliseconds()
	go gatewaystore.UpdateChannelResponseTime(channel, milliseconds)
	return float64(milliseconds) / 1000.0, result.newAPIError, nil
}

// MarketplaceChannelTestOptions binds a user test to the exact market group
// selected in the UI. It is deliberately separate from automatic probes so a
// background health check can never spend a user's balance.
type MarketplaceChannelTestOptions struct {
	UserID             int
	MarketplaceGroupID string
	InternalGroup      string
	MarketplaceOwnerID int
	CreditPoolPolicy   string
	Multiplier         float64
	ModelPrices        map[string]marketplacedomain.ChannelModelPrice
}

// TestMarketplaceChannelByID executes a real upstream request as a user and
// settles it through the normal wallet/subscription billing pipeline.
func TestMarketplaceChannelByID(channelID int, testModel string, endpointType string, isStream bool, options MarketplaceChannelTestOptions) (float64, ChannelTestReport, *types.NewAPIError, error) {
	channel, err := getChannelForTest(channelID)
	if err != nil {
		return 0, ChannelTestReport{}, nil, err
	}
	if options.UserID <= 0 {
		return 0, ChannelTestReport{}, nil, errors.New("用户 ID 无效")
	}
	tik := time.Now()
	result := testChannelWithOptions(channel, testModel, endpointType, isStream, channelTestOptions{
		UserID:                 options.UserID,
		BillUser:               true,
		MarketplaceGroupID:     options.MarketplaceGroupID,
		InternalGroup:          options.InternalGroup,
		MarketplaceOwnerID:     options.MarketplaceOwnerID,
		CreditPoolPolicy:       options.CreditPoolPolicy,
		MarketplaceMultiplier:  options.Multiplier,
		MarketplaceModelPrices: options.ModelPrices,
	})
	if result.localErr != nil {
		return 0, result.report, result.newAPIError, result.localErr
	}
	milliseconds := time.Since(tik).Milliseconds()
	go gatewaystore.UpdateChannelResponseTime(channel, milliseconds)
	return float64(milliseconds) / 1000.0, result.report, result.newAPIError, nil
}

// TestAllChannels starts the asynchronous full-channel test job.
func TestAllChannels(notify bool) error {
	testAllChannelsLock.Lock()
	if testAllChannelsRunning {
		testAllChannelsLock.Unlock()
		return errors.New("测试已在运行中")
	}
	testAllChannelsRunning = true
	testAllChannelsLock.Unlock()

	channels, err := gatewaystore.ListAllChannels(0, 0, true, false)
	if err != nil {
		testAllChannelsLock.Lock()
		testAllChannelsRunning = false
		testAllChannelsLock.Unlock()
		return err
	}

	disableThreshold := int64(platformconfig.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000
	}

	gopool.Go(func() {
		defer func() {
			testAllChannelsLock.Lock()
			testAllChannelsRunning = false
			testAllChannelsLock.Unlock()
		}()

		for _, channel := range channels {
			if channel.Status == constant.ChannelStatusManuallyDisabled {
				continue
			}

			isChannelEnabled := channel.Status == constant.ChannelStatusEnabled
			tik := time.Now()
			result := testChannel(channel, "", "", shouldUseStreamForAutomaticChannelTest(channel))
			milliseconds := time.Since(tik).Milliseconds()

			shouldBanChannel := false
			newAPIError := result.newAPIError
			if newAPIError != nil {
				shouldBanChannel = ShouldDisableChannel(newAPIError)
			}
			if platformconfig.AutomaticDisableChannelEnabled && !shouldBanChannel && milliseconds > disableThreshold {
				err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
				newAPIError = types.NewOpenAIError(err, types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
				shouldBanChannel = true
			}

			if isChannelEnabled && shouldBanChannel && channel.GetAutoBan() {
				ProcessChannelError(
					result.context,
					*types.NewChannelError(
						channel.Id,
						channel.Type,
						channel.Name,
						channel.ChannelInfo.IsMultiKey,
						httpctx.GetContextKeyString(result.context, constant.ContextKeyChannelKey),
						channel.GetAutoBan(),
					),
					newAPIError,
				)
			}

			if !isChannelEnabled && ShouldEnableChannel(newAPIError, channel.Status) {
				EnableChannel(channel.Id, httpctx.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.Name)
			}

			gatewaystore.UpdateChannelResponseTime(channel, milliseconds)
			time.Sleep(platformconfig.RequestInterval)
		}

		if notify {
			notifyx.NotifyRootUser(dto.NotifyTypeChannelTest, "通道测试完成", "所有通道测试已完成")
		}
	})

	return nil
}

// StartAutomaticChannelTestTask starts the periodic automatic channel testing loop once.
func StartAutomaticChannelTestTask() {
	if !platformconfig.IsMasterNode {
		return
	}

	autoTestChannelsOnce.Do(func() {
		go func() {
			for {
				if !gatewaystore.GetMonitorSetting().AutoTestChannelEnabled {
					time.Sleep(time.Minute)
					continue
				}

				for {
					frequency := gatewaystore.GetMonitorSetting().AutoTestChannelMinutes
					time.Sleep(time.Duration(int(math.Round(frequency))) * time.Minute)
					platformobservability.SysLog(fmt.Sprintf("automatically test channels with interval %f minutes", frequency))
					platformobservability.SysLog("automatically testing all channels")
					_ = TestAllChannels(false)
					platformobservability.SysLog("automatically channel test finished")
					if !gatewaystore.GetMonitorSetting().AutoTestChannelEnabled {
						break
					}
				}
			}
		}()
	})
}
