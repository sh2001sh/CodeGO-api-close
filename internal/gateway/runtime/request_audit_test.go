package runtime

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRequestAuditPersistsFinalRequestAndEveryAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:request-audit-test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&gatewayschema.RequestAudit{}, &gatewayschema.RequestAttemptAudit{}))

	previousDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = previousDB })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(constant.RequestIdKey, "request-audit-test")
	ctx.Set("original_model", "gpt-5.6")
	ctx.Set("id", 42)
	ctx.Set("token_id", 7)
	ctx.Set("group", "177-Codex Pro")
	ctx.Set(string(constant.ContextKeyRequestStartTime), time.Now().Add(-time.Second))
	InitializeRequestProfile(ctx, "gpt-5.6", "/v1/responses", RequestProfileHint{IsStream: true})
	StartRouteDecision(ctx, "gpt-5.6", "177-Codex Pro")
	StartRouteDecisionAttempt(ctx, 0, 11, "provider:a")
	FinishRouteDecisionAttempt(ctx, false, 502, "upstream_transient", "response")
	StartRouteDecisionAttempt(ctx, 1, 12, "provider:b")
	FinishRouteDecisionAttempt(ctx, true, 0, "", "stream")

	FinalizeRequestAudit(ctx, nil, nil, true, true)
	require.Eventually(t, func() bool {
		var request gatewayschema.RequestAudit
		if db.Where("request_id = ?", "request-audit-test").First(&request).Error != nil {
			return false
		}
		var attempts int64
		if db.Model(&gatewayschema.RequestAttemptAudit{}).Where("request_id = ?", "request-audit-test").Count(&attempts).Error != nil {
			return false
		}
		return request.Status == gatewayschema.RequestAuditStatusSucceeded && request.AttemptsCount == 2 && attempts == 2
	}, time.Second, 10*time.Millisecond)
}

func TestRequestAuditClassifiesPreUpstreamFailureAsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:request-audit-rejected-test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.RequestAudit{}))
	previousDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = previousDB })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(constant.RequestIdKey, "request-audit-rejected")
	ctx.Set("id", 42)
	apiErr := types.NewError(errors.New("invalid request"), types.ErrorCodeInvalidRequest)
	FinalizeRequestAudit(ctx, nil, apiErr, false, false)

	require.Eventually(t, func() bool {
		var request gatewayschema.RequestAudit
		return db.Where("request_id = ?", "request-audit-rejected").First(&request).Error == nil &&
			request.Status == gatewayschema.RequestAuditStatusRejected
	}, time.Second, 10*time.Millisecond)
}
