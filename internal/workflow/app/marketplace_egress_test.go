package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/internal/workflow/providers/taskcommon"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

const isolatedOwnerMetadata = `{"marketplace_channel_id":"owner-channel","marketplace_group_id":"owner-group","source_type":"marketplace_user"}`

func TestMarketplaceWorkflowRestoresEgressForRealVideoAdaptorsAndWorkerPoll(t *testing.T) {
	truncateTables(t)
	previousCache, previousPolling, previousRelay := platformconfig.MemoryCacheEnabled, GetTaskAdaptorFunc, GetTaskRelayAdaptorFunc
	platformconfig.MemoryCacheEnabled = false
	GetTaskAdaptorFunc, GetTaskRelayAdaptorFunc = NewTaskPollingAdaptor, NewTaskRelayAdaptor
	t.Cleanup(func() {
		platformconfig.MemoryCacheEnabled = previousCache
		GetTaskAdaptorFunc = previousPolling
		GetTaskRelayAdaptorFunc = previousRelay
	})
	platformhttpx.InitHTTPClient()
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeOpenAI} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			var reached atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			channel := &gatewayschema.Channel{Id: 990000 + channelType, Type: channelType, BaseURL: &server.URL, Key: "isolated-credential", Status: constant.ChannelStatusEnabled,
				ChannelScope: gatewayschema.ChannelScopeExternal, OtherInfo: isolatedOwnerMetadata}
			require.NoError(t, platformdb.DB.Create(channel).Error)
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType, ChannelBaseUrl: server.URL, ApiKey: channel.Key, MarketplaceEgress: channel.RequiresMarketplaceEgress()}}
			info.UpstreamModelName = "veo-3.0-generate-001"
			platform := constant.TaskPlatform(strconv.Itoa(channelType))
			adaptor := NewTaskRelayAdaptor(platform)
			adaptor.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			_, err := adaptor.DoRequest(c, info, strings.NewReader(`{"prompt":"isolated"}`))
			require.Error(t, err)
			upstreamID := "isolated-video"
			if channelType == constant.ChannelTypeGemini {
				upstreamID = taskcommon.EncodeLocalTaskID("models/veo-3.0-generate-001/operations/isolated")
			}
			_, err = adaptor.FetchTask(server.URL, channel.Key, map[string]any{"task_id": upstreamID}, "")
			require.Error(t, err)
			task := &workflowschema.Task{TaskID: fmt.Sprintf("isolated-%d", channelType), Platform: platform, ChannelId: channel.Id, Status: workflowschema.TaskStatusInProgress,
				PrivateData: workflowschema.TaskPrivateData{UpstreamTaskID: upstreamID, Key: channel.Key}}
			// This is the worker entry used by the Temporal activity. It reloads
			// the persisted channel and creates a new adaptor, rather than relying
			// on submission context or an in-memory policy flag.
			require.Error(t, PollSingleTask(context.Background(), task))
			if channelType == constant.ChannelTypeGemini {
				require.Empty(t, tryRealtimeFetch(task, true))
				_, err = getGeminiVideoURL(channel, task, channel.Key)
				require.Error(t, err)
			}
			require.Zero(t, reached.Load(), "submit, direct poll, restored worker poll and video resolution must reject private targets")
			// The same provider's administrator-created upstream remains usable.
			info.MarketplaceEgress = false
			adaptor.Init(info)
			response, err := adaptor.FetchTask(server.URL, channel.Key, map[string]any{"task_id": upstreamID}, "")
			require.NoError(t, err)
			_ = response.Body.Close()
			require.EqualValues(t, 1, reached.Load())
		})
	}
}

func TestMarketplaceRemixRestoresOriginChannelEgressWithoutBillingBinding(t *testing.T) {
	truncateTables(t)
	previousCache := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = false
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = previousCache })
	platformhttpx.InitHTTPClient()
	for _, owner := range []bool{true, false} {
		for _, sameChannel := range []bool{false, true} {
			t.Run(fmt.Sprintf("owner=%t/same=%t", owner, sameChannel), func(t *testing.T) {
				var reached atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					reached.Add(1)
					_, _ = w.Write([]byte(`{"id":"isolated-video"}`))
				}))
				defer server.Close()
				channel := &gatewayschema.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &server.URL, Key: "isolated-credential", Status: constant.ChannelStatusEnabled, ChannelScope: gatewayschema.ChannelScopeExternal}
				if owner {
					channel.OtherInfo = isolatedOwnerMetadata
				}
				require.NoError(t, platformdb.DB.Create(channel).Error)
				task := &workflowschema.Task{TaskID: fmt.Sprintf("remix-%d", channel.Id), UserId: 99001, ChannelId: channel.Id, Status: workflowschema.TaskStatusSuccess,
					PrivateData: workflowschema.TaskPrivateData{UpstreamTaskID: "isolated-video"}}
				require.NoError(t, platformdb.DB.Create(task).Error)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/"+task.TaskID+"/remix", nil)
				c.Params = gin.Params{{Key: "video_id", Value: task.TaskID}}
				httpctx.SetContextKey(c, constant.ContextKeyUserId, task.UserId)
				selectedID := channel.Id + 1000000
				if sameChannel {
					selectedID = channel.Id
				}
				httpctx.SetContextKey(c, constant.ContextKeyChannelId, selectedID)
				httpctx.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
				httpctx.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
				httpctx.SetContextKey(c, constant.ContextKeyChannelKey, channel.Key)
				httpctx.SetContextKey(c, constant.ContextKeyChannelMarketplaceEgress, !owner)
				info, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
				require.NoError(t, err)
				require.Nil(t, info.ChannelMeta, "use the actual submission initialization order")
				require.Nil(t, ResolveOriginTask(c, info))
				info.InitChannelMeta(c)
				require.Equal(t, owner, info.UsesMarketplaceEgress())
				require.Equal(t, channel.Id, info.ChannelId)
				require.Empty(t, info.MarketplaceGroupID)
				require.Zero(t, info.MarketplaceOwnerID)
				require.Empty(t, info.MarketplaceSourceType)
				adaptor := NewTaskRelayAdaptor(constant.TaskPlatform(strconv.Itoa(channel.Type)))
				adaptor.Init(info)
				response, err := adaptor.DoRequest(c, info, strings.NewReader(`{"prompt":"isolated"}`))
				if owner {
					require.Error(t, err)
					require.Zero(t, reached.Load())
				} else {
					require.NoError(t, err)
					_ = response.Body.Close()
					require.EqualValues(t, 1, reached.Load())
				}
			})
		}
	}
}

func TestMarketplaceVideoContentUsesStrictClientWhileAdminLoopbackStreams(t *testing.T) {
	truncateTables(t)
	previousCache := platformconfig.MemoryCacheEnabled
	platformconfig.MemoryCacheEnabled = false
	t.Cleanup(func() { platformconfig.MemoryCacheEnabled = previousCache })
	setting := platformstore.GetFetchSetting()
	previousSetting := *setting
	setting.EnableSSRFProtection = false
	t.Cleanup(func() { *setting = previousSetting })
	platformhttpx.InitHTTPClient()
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeGemini} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			var reached atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached.Add(1)
				if channelType == constant.ChannelTypeOpenAI {
					require.Equal(t, "Bearer isolated-credential", r.Header.Get("Authorization"))
				} else {
					require.Equal(t, "isolated-credential", r.Header.Get("X-Goog-Api-Key"))
				}
				_, _ = w.Write([]byte("isolated-video-bytes"))
			}))
			defer server.Close()
			channel := &gatewayschema.Channel{Id: 991000 + channelType, Type: channelType, BaseURL: &server.URL, Key: "isolated-credential", Status: constant.ChannelStatusEnabled,
				ChannelScope: gatewayschema.ChannelScopeExternal, OtherInfo: isolatedOwnerMetadata}
			require.NoError(t, platformdb.DB.Create(channel).Error)
			data, err := json.Marshal(map[string]string{"uri": server.URL + "/video-content"})
			require.NoError(t, err)
			task := &workflowschema.Task{TaskID: fmt.Sprintf("content-%d", channelType), UserId: 99001, ChannelId: channel.Id, Status: workflowschema.TaskStatusSuccess,
				PrivateData: workflowschema.TaskPrivateData{UpstreamTaskID: "isolated-video", Key: channel.Key}, Data: data}
			require.NoError(t, platformdb.DB.Create(task).Error)
			invoke := func() *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
				c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
				c.Set("id", task.UserId)
				ProxyVideoContent(c)
				return recorder
			}
			require.Equal(t, http.StatusBadGateway, invoke().Code)
			require.Zero(t, reached.Load(), "the configurable fetch SSRF toggle must not disable owner egress policy")
			require.NoError(t, platformdb.DB.Model(channel).Update("other_info", "").Error)
			response := invoke()
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "isolated-video-bytes", response.Body.String())
			require.EqualValues(t, 1, reached.Load())
		})
	}
}
