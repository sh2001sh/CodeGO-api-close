package http

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	routepin "github.com/sh2001sh/new-api/internal/gateway/routepin"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	"github.com/sh2001sh/new-api/types"
)

const (
	responsesBackgroundScanInterval = 2 * time.Second
	responsesBackgroundJobTimeout   = 2 * time.Hour
	responsesBackgroundWorkerCount  = 4
)

var (
	responsesBackgroundWorkerOnce sync.Once
	responsesBackgroundSlots      = make(chan struct{}, responsesBackgroundWorkerCount)
	enqueueResponsesBackgroundJob = queueResponsesBackgroundJob
)

// StartResponsesBackgroundWorker resumes only queued jobs. Jobs already
// claimed as in_progress are deliberately not replayed after a process crash.
func StartResponsesBackgroundWorker() {
	responsesBackgroundWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(responsesBackgroundScanInterval)
			defer ticker.Stop()
			for {
				dispatchQueuedResponsesBackgroundJobs()
				<-ticker.C
			}
		}()
	})
}

func queueResponsesBackgroundJob(jobID string) {
	StartResponsesBackgroundWorker()
	dispatchResponsesBackgroundJob(jobID)
}

func dispatchQueuedResponsesBackgroundJobs() {
	jobs, err := gatewaystore.ListQueuedResponsesBackgroundJobs(responsesBackgroundWorkerCount * 2)
	if err != nil {
		platformobservability.SysLog("failed to scan queued background responses: " + err.Error())
		return
	}
	for _, job := range jobs {
		dispatchResponsesBackgroundJob(job.ID)
	}
}

func dispatchResponsesBackgroundJob(jobID string) {
	select {
	case responsesBackgroundSlots <- struct{}{}:
		go func() {
			defer func() { <-responsesBackgroundSlots }()
			executeResponsesBackgroundJob(jobID)
		}()
	default:
	}
}

func executeResponsesBackgroundJob(jobID string) {
	now := time.Now().UTC()
	claimed, err := gatewaystore.ClaimResponsesBackgroundJob(jobID, now)
	if err != nil || !claimed {
		return
	}
	job, err := gatewaystore.LoadResponsesBackgroundJob(jobID)
	if err != nil {
		return
	}
	requestBody, err := platformsecurity.DecryptSecret(job.RequestCiphertext)
	if err != nil {
		completeResponsesBackgroundFailure(job, err)
		return
	}
	authorization, err := platformsecurity.DecryptSecret(job.AuthorizationCiphertext)
	if err != nil {
		completeResponsesBackgroundFailure(job, err)
		return
	}
	clientIP, err := platformsecurity.DecryptSecret(job.ClientIPCiphertext)
	if err != nil {
		completeResponsesBackgroundFailure(job, err)
		return
	}

	executionCtx, cancel := context.WithTimeout(context.Background(), responsesBackgroundJobTimeout)
	defer cancel()
	var canceled atomic.Bool
	monitorDone := make(chan struct{})
	go monitorResponsesBackgroundCancel(executionCtx, job.ID, &canceled, cancel, monitorDone)

	writer := newResponsesBackgroundWriter(job)
	request, err := http.NewRequestWithContext(executionCtx, http.MethodPost, "/v1/responses", bytes.NewReader([]byte(requestBody)))
	if err != nil {
		close(monitorDone)
		completeResponsesBackgroundFailure(job, err)
		return
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json")
	if clientIP != "" {
		request.RemoteAddr = net.JoinHostPort(clientIP, "0")
	}
	buildResponsesBackgroundExecutionRouter(job).ServeHTTP(writer, request)
	close(monitorDone)
	if err := writer.Finish(canceled.Load()); err != nil {
		platformobservability.SysLog(fmt.Sprintf("failed to finish background response: job_id=%s error=%v", job.ID, err))
	}
}

func buildResponsesBackgroundExecutionRouter(job *gatewayschema.ResponsesBackgroundJob) *gin.Engine {
	router := gin.New()
	router.POST("/v1/responses",
		func(c *gin.Context) {
			defer platformhttpx.CleanupBodyStorage(c)
			c.Next()
		},
		middleware.TokenAuth(),
		func(c *gin.Context) {
			routepin.Attach(c, routepin.Pin{ChannelID: job.ChannelID, KeyIndex: job.KeyIndex})
			if err := restoreResponsesBackgroundRoutingContext(c, job.RoutingContextCiphertext); err != nil {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to restore background routing context."}})
				return
			}
			c.Set(constant.RequestIdKey, job.ID)
			c.Set(constant.TraceIdKey, job.ID)
			httpctx.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
			c.Next()
		},
		middleware.Distribute(),
		func(c *gin.Context) {
			relayRequest(c, types.RelayFormatOpenAIResponses)
		},
	)
	return router
}

func monitorResponsesBackgroundCancel(ctx context.Context, jobID string, canceled *atomic.Bool, cancel context.CancelFunc, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			requested, err := gatewaystore.ResponsesBackgroundCancelRequested(jobID)
			if err == nil && requested {
				canceled.Store(true)
				cancel()
				return
			}
		}
	}
}

func completeResponsesBackgroundFailure(job *gatewayschema.ResponsesBackgroundJob, executionErr error) {
	if job == nil {
		return
	}
	errorValue := map[string]any{
		"type": "server_error", "code": "background_execution_failed",
		"message": "Background response execution failed.",
	}
	_ = appendSyntheticBackgroundTerminal(job, gatewayschema.ResponsesBackgroundFailed, "response.failed", errorValue)
	response := syntheticBackgroundResponse(job, gatewayschema.ResponsesBackgroundFailed, errorValue)
	responseRaw, _ := platformencoding.Marshal(response)
	errorRaw, _ := platformencoding.Marshal(errorValue)
	responseCiphertext, _ := platformsecurity.EncryptSecret(string(responseRaw))
	errorCiphertext, _ := platformsecurity.EncryptSecret(string(errorRaw))
	_ = gatewaystore.UpdateResponsesBackgroundTerminal(
		job.ID, gatewayschema.ResponsesBackgroundFailed, responseCiphertext, errorCiphertext, "", time.Now().UTC(),
	)
	platformobservability.SysLog(fmt.Sprintf("background response execution failed: job_id=%s error=%v", job.ID, executionErr))
}
