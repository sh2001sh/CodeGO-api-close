package http

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/sh2001sh/new-api/types"
	"gorm.io/gorm"
)

const backgroundEventPollInterval = 250 * time.Millisecond

// ResponsesCreate routes ordinary requests through the synchronous relay and
// persists background requests before starting their asynchronous execution.
func ResponsesCreate(c *gin.Context) {
	var request dto.OpenAIResponsesRequest
	if err := platformhttpx.UnmarshalBodyReusable(c, &request); err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	if request.Background == nil || !*request.Background {
		relayRequest(c, types.RelayFormatOpenAIResponses)
		return
	}
	createResponsesBackground(c, &request)
}

func ResponsesCreateWithCanonicalPath(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request != nil && c.Request.URL != nil {
			c.Request.URL.Path = path
		}
		ResponsesCreate(c)
	}
}

func createResponsesBackground(c *gin.Context, request *dto.OpenAIResponsesRequest) {
	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry()))
		return
	}
	raw, err := storage.Bytes()
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry()))
		return
	}
	previousJob, previousResponseID, err := resolveResponsesBackgroundPrevious(c, request.PreviousResponseID)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	executionRequest, err := normalizeBackgroundExecutionRequest(raw, previousResponseID)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	requestCiphertext, err := platformsecurity.EncryptSecret(string(executionRequest))
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry()))
		return
	}
	authorizationCiphertext, err := platformsecurity.EncryptSecret(c.GetHeader("Authorization"))
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry()))
		return
	}
	clientIPCiphertext, err := platformsecurity.EncryptSecret(c.ClientIP())
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry()))
		return
	}
	channelID := httpctx.GetContextKeyInt(c, constant.ContextKeyChannelId)
	keyIndex := httpctx.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	var routingContextCiphertext string
	if previousJob != nil {
		channelID = previousJob.ChannelID
		keyIndex = previousJob.KeyIndex
		routingContextCiphertext = previousJob.RoutingContextCiphertext
	} else {
		routingContextCiphertext, err = captureResponsesBackgroundRoutingContext(c)
		if err != nil {
			respondRelayError(c, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry()))
			return
		}
	}
	stream := request.Stream != nil && *request.Stream
	job := &gatewayschema.ResponsesBackgroundJob{
		ID:     "resp_bg_" + strings.ReplaceAll(platformruntime.GetUUID(), "-", ""),
		UserID: httpctx.GetContextKeyInt(c, constant.ContextKeyUserId), TokenID: httpctx.GetContextKeyInt(c, constant.ContextKeyTokenId),
		Model: request.Model, Status: gatewayschema.ResponsesBackgroundQueued, Stream: stream,
		ChannelID:         channelID,
		KeyIndex:          keyIndex,
		RequestCiphertext: requestCiphertext, AuthorizationCiphertext: authorizationCiphertext,
		ClientIPCiphertext: clientIPCiphertext, RoutingContextCiphertext: routingContextCiphertext, LastSequence: -1,
	}
	if job.UserID <= 0 || job.TokenID <= 0 || job.ChannelID <= 0 {
		respondRelayError(c, types.NewError(errors.New("background request context is incomplete"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	if err := gatewaystore.CreateResponsesBackgroundJob(job); err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry()))
		return
	}
	enqueueResponsesBackgroundJob(job.ID)
	if stream {
		streamResponsesBackgroundEvents(c, job, -1)
		return
	}
	c.JSON(http.StatusOK, backgroundJobResponse(job))
}

// GetResponsesBackground returns a current response snapshot or resumes its
// persisted event stream after the supplied local sequence cursor.
func GetResponsesBackground(c *gin.Context) {
	job, err := loadOwnedBackgroundJob(c)
	if err != nil {
		respondBackgroundLookupError(c, err)
		return
	}
	if !queryBool(c.Query("stream")) {
		c.JSON(http.StatusOK, backgroundJobResponse(job))
		return
	}
	if !job.Stream {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type": "invalid_request_error", "code": "background_stream_not_enabled",
			"message": "This background response was not created with stream=true.",
		}})
		return
	}
	startingAfter := int64(-1)
	if raw := strings.TrimSpace(c.Query("starting_after")); raw != "" {
		startingAfter, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || startingAfter < -1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "starting_after must be an integer sequence number"}})
			return
		}
	}
	streamResponsesBackgroundEvents(c, job, startingAfter)
}

// CancelResponsesBackground requests cancellation while preserving ownership
// checks by both user and API token.
func CancelResponsesBackground(c *gin.Context) {
	now := time.Now().UTC()
	job, err := gatewaystore.RequestResponsesBackgroundCancel(
		c.Param("id"),
		httpctx.GetContextKeyInt(c, constant.ContextKeyUserId),
		httpctx.GetContextKeyInt(c, constant.ContextKeyTokenId),
		now,
	)
	if err != nil {
		respondBackgroundLookupError(c, err)
		return
	}
	if job.Status == gatewayschema.ResponsesBackgroundCanceled && job.LastSequence < 0 {
		_ = appendSyntheticBackgroundTerminal(job, gatewayschema.ResponsesBackgroundCanceled, "response.cancelled", nil)
		job, _ = gatewaystore.LoadResponsesBackgroundJob(job.ID)
	}
	c.JSON(http.StatusOK, backgroundJobResponse(job))
}

func loadOwnedBackgroundJob(c *gin.Context) (*gatewayschema.ResponsesBackgroundJob, error) {
	return gatewaystore.LoadOwnedResponsesBackgroundJob(
		c.Param("id"),
		httpctx.GetContextKeyInt(c, constant.ContextKeyUserId),
		httpctx.GetContextKeyInt(c, constant.ContextKeyTokenId),
	)
}

func respondBackgroundLookupError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": http.StatusText(status)}})
}

func streamResponsesBackgroundEvents(c *gin.Context, job *gatewayschema.ResponsesBackgroundJob, cursor int64) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	ticker := time.NewTicker(backgroundEventPollInterval)
	defer ticker.Stop()
	for {
		events, err := gatewaystore.ListResponsesBackgroundEvents(job.ID, cursor, 200)
		if err != nil {
			return
		}
		for _, event := range events {
			payload, decryptErr := platformsecurity.DecryptSecret(event.PayloadCiphertext)
			if decryptErr != nil {
				return
			}
			_, _ = io.WriteString(c.Writer, "event: "+event.Type+"\ndata: "+payload+"\n\n")
			cursor = event.Sequence
			if flusher != nil {
				flusher.Flush()
			}
		}
		current, err := gatewaystore.LoadResponsesBackgroundJob(job.ID)
		if err != nil {
			return
		}
		if gatewayschema.IsResponsesBackgroundTerminal(current.Status) && cursor >= current.LastSequence {
			return
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func normalizeBackgroundExecutionRequest(raw []byte, previousResponseID string) ([]byte, error) {
	var request map[string]any
	if err := platformencoding.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	request["background"] = false
	request["stream"] = true
	if previousResponseID != "" {
		request["previous_response_id"] = previousResponseID
	}
	return platformencoding.Marshal(request)
}

func resolveResponsesBackgroundPrevious(c *gin.Context, previousResponseID string) (*gatewayschema.ResponsesBackgroundJob, string, error) {
	previousResponseID = strings.TrimSpace(previousResponseID)
	if previousResponseID == "" || !strings.HasPrefix(previousResponseID, "resp_bg_") {
		return nil, previousResponseID, nil
	}
	job, err := gatewaystore.LoadOwnedResponsesBackgroundJob(
		previousResponseID,
		httpctx.GetContextKeyInt(c, constant.ContextKeyUserId),
		httpctx.GetContextKeyInt(c, constant.ContextKeyTokenId),
	)
	if err != nil {
		return nil, "", errors.New("previous background response was not found")
	}
	if job.Status != gatewayschema.ResponsesBackgroundCompleted || strings.TrimSpace(job.UpstreamResponseID) == "" {
		return nil, "", errors.New("previous background response is not completed")
	}
	return job, job.UpstreamResponseID, nil
}

func queryBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}
