package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

func TestMarketplaceBackgroundCancelRestoresChannelEgressWithoutGroupBinding(t *testing.T) {
	db := setupResponsesBackgroundTestDB(t)
	platformhttpx.InitHTTPClient()
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/responses/resp_isolated/cancel", r.URL.Path)
		require.Equal(t, "Bearer isolated-credential", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	channel := &gatewayschema.Channel{Id: 99001, Type: constant.ChannelTypeOpenAI, Key: "isolated-credential", BaseURL: &server.URL, Status: constant.ChannelStatusEnabled,
		ChannelScope: gatewayschema.ChannelScopeExternal, OtherInfo: `{"marketplace_channel_id":"owner-channel","marketplace_group_id":"owner-group","source_type":"marketplace_user"}`}
	require.NoError(t, db.Create(channel).Error)
	job := &gatewayschema.ResponsesBackgroundJob{ChannelID: channel.Id, UpstreamResponseID: "resp_isolated"}
	require.ErrorContains(t, forwardNativeBackgroundCancel(context.Background(), job), "marketplace background cancel request failed")
	require.Zero(t, reached.Load())
	require.NoError(t, db.Model(channel).Update("other_info", "").Error)
	require.NoError(t, forwardNativeBackgroundCancel(context.Background(), job))
	require.EqualValues(t, 1, reached.Load())
}
