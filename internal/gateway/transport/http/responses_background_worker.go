package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
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
	responsesBackgroundLease        = 30 * time.Second
	responsesBackgroundHeartbeat    = 10 * time.Second
)

var (
	responsesBackgroundWorkerOnce sync.Once
	responsesBackgroundSlots      = make(chan struct{}, responsesBackgroundWorkerCount)
	enqueueResponsesBackgroundJob = queueResponsesBackgroundJob
)

// StartResponsesBackgroundWorker claims queued work and resumes native jobs
// whose previous process stopped renewing its lease.
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
	jobs, err := gatewaystore.ListRecoverableResponsesBackgroundJobs(
		responsesBackgroundWorkerCount*2,
		time.Now().UTC().Add(-responsesBackgroundLease),
	)
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
	claimed, err := gatewaystore.ClaimResponsesBackgroundJobWithLease(jobID, now, responsesBackgroundLease)
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
			if job.NativeBackground {
				c.Set(string(constant.ContextKeyNativeBackground), true)
				if job.UpstreamResponseID != "" {
					c.Set(string(constant.ContextKeyBackgroundResumeID), job.UpstreamResponseID)
					c.Set(string(constant.ContextKeyBackgroundResumeCursor), job.UpstreamSequence)
				}
			}
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
	lastHeartbeat := time.Now()
	var cancelRequestedAt time.Time
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastHeartbeat) >= responsesBackgroundHeartbeat {
				_ = gatewaystore.RenewResponsesBackgroundLease(jobID, time.Now().UTC())
				lastHeartbeat = time.Now()
			}
			requested, err := gatewaystore.ResponsesBackgroundCancelRequested(jobID)
			if err == nil && requested {
				if cancelRequestedAt.IsZero() {
					cancelRequestedAt = time.Now()
				}
				job, loadErr := gatewaystore.LoadResponsesBackgroundJob(jobID)
				if loadErr == nil && job.NativeBackground && job.UpstreamResponseID != "" {
					if cancelErr := forwardNativeBackgroundCancel(ctx, job); cancelErr != nil {
						platformobservability.SysLog(fmt.Sprintf("failed to cancel native background response: job_id=%s error=%v", jobID, cancelErr))
					}
					canceled.Store(true)
					cancel()
					return
				}
				if loadErr != nil || !job.NativeBackground || time.Since(cancelRequestedAt) >= 10*time.Second {
					canceled.Store(true)
					cancel()
					return
				}
			}
		}
	}
}

func forwardNativeBackgroundCancel(parent context.Context, job *gatewayschema.ResponsesBackgroundJob) error {
	if job == nil || job.ChannelID <= 0 || strings.TrimSpace(job.UpstreamResponseID) == "" {
		return fmt.Errorf("background cancel route is incomplete")
	}
	channel, err := gatewaystore.LoadChannelByID(job.ChannelID, true)
	if err != nil {
		return err
	}
	key, apiErr := gatewaystore.GetEnabledChannelKeyByIndex(channel, job.KeyIndex)
	if apiErr != nil {
		return apiErr
	}
	endpoint, err := nativeBackgroundEndpoint(channel.GetBaseURL(), job.UpstreamResponseID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/cancel", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	for name, value := range gatewaydomain.GetHeaderOverride(channel) {
		request.Header.Set(name, fmt.Sprint(value))
	}
	setting := gatewaydomain.GetSettings(channel)
	var client *http.Client
	if setting.Proxy != "" {
		client, err = platformhttpx.NewProxyHTTPClientWithResponseHeaderTimeout(setting.Proxy, 20*time.Second)
	} else {
		client = platformhttpx.GetHTTPClientWithResponseHeaderTimeout(20 * time.Second)
	}
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("background cancel rejected with status %d", response.StatusCode)
	}
	return nil
}

func nativeBackgroundEndpoint(baseURL, responseID string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid background channel base URL")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/v1/responses") {
		if strings.HasSuffix(path, "/v1") {
			path += "/responses"
		} else {
			path += "/v1/responses"
		}
	}
	parsed.Path = path + "/" + url.PathEscape(responseID)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
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
