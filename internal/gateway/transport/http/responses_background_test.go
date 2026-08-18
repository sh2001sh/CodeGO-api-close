package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResponsesBackgroundCreatePersistsEncryptedPinnedJob(t *testing.T) {
	db := setupResponsesBackgroundTestDB(t)
	originalEnqueue := enqueueResponsesBackgroundJob
	queued := ""
	enqueueResponsesBackgroundJob = func(jobID string) { queued = jobID }
	t.Cleanup(func() { enqueueResponsesBackgroundJob = originalEnqueue })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hello","background":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer sk-test")
	c.Request.RemoteAddr = "127.0.0.1:1234"
	httpctx.SetContextKey(c, constant.ContextKeyUserId, 11)
	httpctx.SetContextKey(c, constant.ContextKeyTokenId, 22)
	httpctx.SetContextKey(c, constant.ContextKeyChannelId, 33)
	httpctx.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 2)
	httpctx.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	httpctx.SetContextKey(c, constant.ContextKeyTokenGroup, "default")

	ResponsesCreate(c)
	defer platformhttpx.CleanupBodyStorage(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotEmpty(t, queued)

	job, err := gatewaystore.LoadResponsesBackgroundJob(queued)
	require.NoError(t, err)
	require.Equal(t, 11, job.UserID)
	require.Equal(t, 22, job.TokenID)
	require.Equal(t, 33, job.ChannelID)
	require.Equal(t, 2, job.KeyIndex)
	require.NotContains(t, job.RequestCiphertext, "hello")
	require.NotContains(t, job.AuthorizationCiphertext, "sk-test")

	raw, err := platformsecurity.DecryptSecret(job.RequestCiphertext)
	require.NoError(t, err)
	var execution map[string]any
	require.NoError(t, platformencoding.Unmarshal([]byte(raw), &execution))
	require.Equal(t, false, execution["background"])
	require.Equal(t, true, execution["stream"])

	var count int64
	require.NoError(t, db.Model(&gatewayschema.ResponsesBackgroundJob{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestResponsesBackgroundCreateUsesNativeCapabilityForSelectedKey(t *testing.T) {
	db := setupResponsesBackgroundTestDB(t)
	baseURL := "https://upstream.example/v1"
	supported := gatewayschema.CapabilityProbeState{
		Status: gatewayschema.CapabilityStatusSupported, Model: "gpt-5", ProbeKeyIdx: 0,
	}
	channel := &gatewayschema.Channel{
		Id: 44, Type: constant.ChannelTypeOpenAI, Key: "key", Models: "gpt-5", BaseURL: &baseURL,
		ChannelInfo: gatewayschema.ChannelInfo{ResponsesCapabilities: gatewayschema.ResponsesCapabilities{
			NativeBackground: supported, BackgroundCreate: supported, BackgroundResume: supported, BackgroundCancel: supported,
		}},
	}
	require.NoError(t, db.Create(channel).Error)
	originalEnqueue := enqueueResponsesBackgroundJob
	queued := ""
	enqueueResponsesBackgroundJob = func(jobID string) { queued = jobID }
	t.Cleanup(func() { enqueueResponsesBackgroundJob = originalEnqueue })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hello","background":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer sk-test")
	httpctx.SetContextKey(c, constant.ContextKeyUserId, 11)
	httpctx.SetContextKey(c, constant.ContextKeyTokenId, 22)
	httpctx.SetContextKey(c, constant.ContextKeyChannelId, 44)
	httpctx.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 0)

	ResponsesCreate(c)
	defer platformhttpx.CleanupBodyStorage(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	job, err := gatewaystore.LoadResponsesBackgroundJob(queued)
	require.NoError(t, err)
	require.True(t, job.NativeBackground)
	raw, err := platformsecurity.DecryptSecret(job.RequestCiphertext)
	require.NoError(t, err)
	var execution map[string]any
	require.NoError(t, platformencoding.Unmarshal([]byte(raw), &execution))
	require.Equal(t, true, execution["background"])
}

func TestResponsesBackgroundPreviousResponseUsesUpstreamIDAndPinnedRoute(t *testing.T) {
	setupResponsesBackgroundTestDB(t)
	previous := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_previous", UserID: 11, TokenID: 22, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundCompleted, ChannelID: 77, KeyIndex: 4,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth",
		RoutingContextCiphertext: "previous-route", UpstreamResponseID: "resp_upstream_previous", LastSequence: 1,
	}
	require.NoError(t, gatewaystore.CreateResponsesBackgroundJob(previous))

	originalEnqueue := enqueueResponsesBackgroundJob
	queued := ""
	enqueueResponsesBackgroundJob = func(jobID string) { queued = jobID }
	t.Cleanup(func() { enqueueResponsesBackgroundJob = originalEnqueue })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(
		`{"model":"gpt-5","input":"continue","background":true,"previous_response_id":"resp_bg_previous"}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer sk-test")
	httpctx.SetContextKey(c, constant.ContextKeyUserId, previous.UserID)
	httpctx.SetContextKey(c, constant.ContextKeyTokenId, previous.TokenID)
	httpctx.SetContextKey(c, constant.ContextKeyChannelId, 999)
	httpctx.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 8)

	ResponsesCreate(c)
	defer platformhttpx.CleanupBodyStorage(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotEmpty(t, queued)

	job, err := gatewaystore.LoadResponsesBackgroundJob(queued)
	require.NoError(t, err)
	require.Equal(t, previous.ChannelID, job.ChannelID)
	require.Equal(t, previous.KeyIndex, job.KeyIndex)
	require.Equal(t, previous.RoutingContextCiphertext, job.RoutingContextCiphertext)
	raw, err := platformsecurity.DecryptSecret(job.RequestCiphertext)
	require.NoError(t, err)
	require.Contains(t, raw, `"previous_response_id":"resp_upstream_previous"`)
}

func TestResponsesBackgroundCancelWinsOverFailedTerminal(t *testing.T) {
	db := setupResponsesBackgroundTestDB(t)
	job := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_cancel_running", UserID: 5, TokenID: 6, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundRunning, ChannelID: 7,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: -1,
	}
	require.NoError(t, db.Create(job).Error)

	writer := newResponsesBackgroundWriter(job)
	writer.Header().Set("Content-Type", "text/event-stream")
	_, err := writer.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_upstream\",\"status\":\"failed\"}}\n\n"))
	require.NoError(t, err)
	_, err = gatewaystore.RequestResponsesBackgroundCancel(job.ID, job.UserID, job.TokenID, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, writer.Finish(false))

	require.NoError(t, db.First(job, "id = ?", job.ID).Error)
	require.Equal(t, gatewayschema.ResponsesBackgroundCanceled, job.Status)
	events, err := gatewaystore.ListResponsesBackgroundEvents(job.ID, -1, 10)
	require.NoError(t, err)
	require.Equal(t, "response.cancelled", events[len(events)-1].Type)
}

func TestResponsesBackgroundWriterPersistsSequenceAndLocalResponseID(t *testing.T) {
	db := setupResponsesBackgroundTestDB(t)
	job := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_local", UserID: 1, TokenID: 2, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundRunning, ChannelID: 3,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: -1,
	}
	require.NoError(t, db.Create(job).Error)
	require.NoError(t, db.First(job, "id = ?", job.ID).Error)

	writer := newResponsesBackgroundWriter(job)
	writer.Header().Set("Content-Type", "text/event-stream")
	_, err := writer.Write([]byte("data: {\"type\":\"response.created\",\"sequence_number\":99,\"response\":{\"id\":\"resp_upstream\",\"status\":\"in_progress\"}}\n\n"))
	require.NoError(t, err)
	_, err = writer.Write([]byte("data: {\"type\":\"response.completed\",\"sequence_number\":100,\"response\":{\"id\":\"resp_upstream\",\"status\":\"completed\",\"output\":[]}}\n\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Finish(false))

	require.NoError(t, db.First(job, "id = ?", job.ID).Error)
	require.Equal(t, gatewayschema.ResponsesBackgroundCompleted, job.Status)
	require.Equal(t, int64(1), job.LastSequence)
	require.Equal(t, "resp_upstream", job.UpstreamResponseID)

	events, err := gatewaystore.ListResponsesBackgroundEvents(job.ID, -1, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, int64(0), events[0].Sequence)
	require.Equal(t, int64(1), events[1].Sequence)
	payload, err := platformsecurity.DecryptSecret(events[1].PayloadCiphertext)
	require.NoError(t, err)
	require.Contains(t, payload, `"id":"resp_bg_local"`)
	require.Contains(t, payload, `"background":true`)
}

func TestResponsesBackgroundStreamResumesAfterCursor(t *testing.T) {
	setupResponsesBackgroundTestDB(t)
	job := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_resume", UserID: 5, TokenID: 6, Model: "gpt-5", Stream: true,
		Status: gatewayschema.ResponsesBackgroundCompleted, ChannelID: 7,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: 1,
	}
	require.NoError(t, gatewaystore.CreateResponsesBackgroundJob(job))
	for sequence, eventType := range []string{"response.created", "response.completed"} {
		payload := `{"type":"` + eventType + `","sequence_number":` + string(rune('0'+sequence)) + `,"response":{"id":"resp_bg_resume"}}`
		ciphertext, err := platformsecurity.EncryptSecret(payload)
		require.NoError(t, err)
		require.NoError(t, gatewaystore.AppendResponsesBackgroundEvent(&gatewayschema.ResponsesBackgroundEvent{
			JobID: job.ID, Sequence: int64(sequence), Type: eventType, PayloadCiphertext: ciphertext,
		}))
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses/"+job.ID+"?stream=true&starting_after=0", nil)
	c.Params = gin.Params{{Key: "id", Value: job.ID}}
	httpctx.SetContextKey(c, constant.ContextKeyUserId, job.UserID)
	httpctx.SetContextKey(c, constant.ContextKeyTokenId, job.TokenID)

	GetResponsesBackground(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "response.created")
	require.Contains(t, recorder.Body.String(), "response.completed")
}

func TestResponsesBackgroundOwnershipIncludesToken(t *testing.T) {
	setupResponsesBackgroundTestDB(t)
	job := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_owned", UserID: 5, TokenID: 6, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundQueued, ChannelID: 7,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: -1,
	}
	require.NoError(t, gatewaystore.CreateResponsesBackgroundJob(job))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses/"+job.ID, nil)
	c.Params = gin.Params{{Key: "id", Value: job.ID}}
	httpctx.SetContextKey(c, constant.ContextKeyUserId, job.UserID)
	httpctx.SetContextKey(c, constant.ContextKeyTokenId, 999)

	GetResponsesBackground(c)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestResponsesBackgroundRoutingContextRoundTrip(t *testing.T) {
	source, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpctx.SetContextKey(source, constant.ContextKeyUsingGroup, "market_u0100")
	httpctx.SetContextKey(source, constant.ContextKeyTokenGroup, "token-group")
	gatewayruntime.MarkAutoRouteRequest(source)
	httpctx.SetContextKey(source, constant.ContextKeyMarketplaceGroupID, "market-group")
	httpctx.SetContextKey(source, constant.ContextKeyMarketplaceOwnerID, 42)
	httpctx.SetContextKey(source, constant.ContextKeyMarketplaceSourceType, "marketplace_user")
	httpctx.SetContextKey(source, constant.ContextKeyMarketplaceCreditPolicy, "subscription_and_universal")
	httpctx.SetContextKey(source, constant.ContextKeyMarketplaceMultiplier, 0.8)
	prices := map[string]marketplaceapp.ChannelModelPrice{
		"gpt-5": {InputPricePerMillion: 1.25, OutputPricePerMillion: 10},
	}
	httpctx.SetContextKey(source, constant.ContextKeyMarketplaceModelPrices, prices)
	gatewayroutingapp.SetRoutePoolSelectionSnapshot(source, gatewayroutingapp.RoutePoolSelection{
		PoolID: 19, ProcurementCostMultiplier: 0.65,
	}, "provider.example.com")

	ciphertext, err := captureResponsesBackgroundRoutingContext(source)
	require.NoError(t, err)
	target, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, restoreResponsesBackgroundRoutingContext(target, ciphertext))

	require.Equal(t, "market_u0100", httpctx.GetContextKeyString(target, constant.ContextKeyUsingGroup))
	require.Equal(t, "token-group", httpctx.GetContextKeyString(target, constant.ContextKeyTokenGroup))
	require.True(t, gatewayruntime.IsAutoRouteRequest(target))
	require.Equal(t, "market-group", httpctx.GetContextKeyString(target, constant.ContextKeyMarketplaceGroupID))
	require.Equal(t, 42, httpctx.GetContextKeyInt(target, constant.ContextKeyMarketplaceOwnerID))
	require.Equal(t, 0.8, httpctx.GetContextKeyFloat64(target, constant.ContextKeyMarketplaceMultiplier))
	restoredPrices, found := httpctx.GetContextKeyType[map[string]marketplaceapp.ChannelModelPrice](target, constant.ContextKeyMarketplaceModelPrices)
	require.True(t, found)
	require.Equal(t, prices, restoredPrices)
	selection, found := gatewayroutingapp.GetRoutePoolSelection(target)
	require.True(t, found)
	require.Equal(t, int64(19), selection.PoolID)
	require.Equal(t, 0.65, selection.ProcurementCostMultiplier)
	require.Equal(t, "provider.example.com", target.GetString("channel_fault_domain"))
}

func setupResponsesBackgroundTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	require.NoError(t, db.AutoMigrate(&gatewayschema.ResponsesBackgroundJob{}, &gatewayschema.ResponsesBackgroundEvent{}, &gatewayschema.Channel{}))
	return db
}
