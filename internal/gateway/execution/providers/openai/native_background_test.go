package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestNativeBackgroundBodyResumesByUpstreamCursorWithoutRecreating(t *testing.T) {
	var posts atomic.Int32
	var resumes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts.Add(1)
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte("data: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_native\"}}\n\n"))
			return
		}
		resumes.Add(1)
		require.Equal(t, "resp_native", strings.TrimPrefix(request.URL.Path, "/v1/responses/"))
		require.Equal(t, "0", request.URL.Query().Get("starting_after"))
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"sequence_number\":1,\"response\":{\"id\":\"resp_native\",\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","background":true,"stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(constant.ContextKeyNativeBackground), true)
	info := &relaycommon.RelayInfo{
		RelayMode: gatewaycontract.RelayModeResponses, IsStream: true,
		RequestURLPath: "/v1/responses", RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "test-key"},
	}
	response, err := doNativeBackgroundRequest(&Adaptor{ChannelType: constant.ChannelTypeOpenAI}, c, info, []byte(`{"model":"gpt-5","background":true,"stream":true}`))
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "response.created")
	require.Contains(t, string(body), "response.completed")
	require.EqualValues(t, 1, posts.Load())
	require.EqualValues(t, 1, resumes.Load())
}

func TestNativeBackgroundRecoveryStartsWithGet(t *testing.T) {
	var posts atomic.Int32
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		gets.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"sequence_number\":4,\"response\":{\"id\":\"resp_recovered\"}}\n\n"))
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","background":true,"stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(constant.ContextKeyNativeBackground), true)
	c.Set(string(constant.ContextKeyBackgroundResumeID), "resp_recovered")
	c.Set(string(constant.ContextKeyBackgroundResumeCursor), int64(3))
	info := &relaycommon.RelayInfo{
		RelayMode: gatewaycontract.RelayModeResponses, IsStream: true,
		RequestURLPath: "/v1/responses", RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "test-key"},
	}
	response, err := doNativeBackgroundRequest(&Adaptor{ChannelType: constant.ChannelTypeOpenAI}, c, info, []byte(`{"model":"gpt-5","background":true,"stream":true}`))
	require.NoError(t, err)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Zero(t, posts.Load())
	require.EqualValues(t, 1, gets.Load())
}
