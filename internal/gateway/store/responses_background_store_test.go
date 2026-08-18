package store

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResponsesBackgroundClaimIsSingleUseAndRunningIsNotQueued(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(&gatewayschema.ResponsesBackgroundJob{}))
	job := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_claim", UserID: 1, TokenID: 2, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundQueued, ChannelID: 3,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: -1,
	}
	require.NoError(t, CreateResponsesBackgroundJob(job))

	claimed, err := ClaimResponsesBackgroundJob(job.ID, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = ClaimResponsesBackgroundJob(job.ID, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, claimed)

	queued, err := ListQueuedResponsesBackgroundJobs(10)
	require.NoError(t, err)
	require.Empty(t, queued)
}

func TestResponsesBackgroundCancelQueuedAndRunning(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(&gatewayschema.ResponsesBackgroundJob{}))

	queued := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_cancel_queued", UserID: 1, TokenID: 2, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundQueued, ChannelID: 3,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: -1,
	}
	running := &gatewayschema.ResponsesBackgroundJob{
		ID: "resp_bg_cancel_running", UserID: 1, TokenID: 2, Model: "gpt-5",
		Status: gatewayschema.ResponsesBackgroundRunning, ChannelID: 3,
		RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route", LastSequence: -1,
	}
	require.NoError(t, db.Create(queued).Error)
	require.NoError(t, db.Create(running).Error)

	canceledQueued, err := RequestResponsesBackgroundCancel(queued.ID, queued.UserID, queued.TokenID, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, gatewayschema.ResponsesBackgroundCanceled, canceledQueued.Status)
	require.True(t, canceledQueued.CancelRequested)

	canceledRunning, err := RequestResponsesBackgroundCancel(running.ID, running.UserID, running.TokenID, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, gatewayschema.ResponsesBackgroundRunning, canceledRunning.Status)
	require.True(t, canceledRunning.CancelRequested)
}

func TestResponsesBackgroundLeaseRecoversOnlyNativeJobWithUpstreamID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(&gatewayschema.ResponsesBackgroundJob{}))
	stale := time.Now().UTC().Add(-time.Minute)
	jobs := []*gatewayschema.ResponsesBackgroundJob{
		{ID: "native", UserID: 1, TokenID: 2, Model: "gpt-5", Status: gatewayschema.ResponsesBackgroundRunning, NativeBackground: true, UpstreamResponseID: "resp_upstream", UpstreamSequence: 3, ClaimedAt: &stale, ChannelID: 3, RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route"},
		{ID: "local", UserID: 1, TokenID: 2, Model: "gpt-5", Status: gatewayschema.ResponsesBackgroundRunning, NativeBackground: false, ClaimedAt: &stale, ChannelID: 3, RequestCiphertext: "request", AuthorizationCiphertext: "auth", RoutingContextCiphertext: "route"},
	}
	require.NoError(t, db.Create(jobs).Error)

	recoverable, err := ListRecoverableResponsesBackgroundJobs(10, time.Now().UTC().Add(-30*time.Second))
	require.NoError(t, err)
	require.Len(t, recoverable, 1)
	require.Equal(t, "native", recoverable[0].ID)
	claimed, err := ClaimResponsesBackgroundJobWithLease("native", time.Now().UTC(), 30*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)
}
