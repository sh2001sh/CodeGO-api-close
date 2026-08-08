package http

import (
	"errors"
	"fmt"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	"github.com/sh2001sh/new-api/internal/platform/transport/http/middleware"
	"github.com/sh2001sh/new-api/types"
)

// RelayWithFormat handles the main synchronous relay entrypoints for a specific protocol shape.
func RelayWithFormat(relayFormat types.RelayFormat) gin.HandlerFunc {
	return func(c *gin.Context) {
		relayRequest(c, relayFormat)
	}
}

// Playground handles authenticated playground text requests.
func Playground(c *gin.Context) {
	if c.GetBool("use_access_token") {
		respondRelayError(c, types.NewError(errors.New("暂不支持使用 access token"), types.ErrorCodeAccessDenied, types.ErrOptionWithSkipRetry()))
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAI, nil, nil)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	userID := c.GetInt("id")
	if err := identityapp.WriteUserCacheToContext(c, userID); err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()))
		return
	}

	tempToken := &identityschema.Token{
		UserId: userID,
		Name:   fmt.Sprintf("playground-%s", relayInfo.UsingGroup),
		Group:  relayInfo.UsingGroup,
	}
	_ = middleware.SetupContextForToken(c, tempToken)

	relayRequest(c, types.RelayFormatOpenAI)
}

// PlaygroundImage handles authenticated image workspace relay requests.
func PlaygroundImage(c *gin.Context) {
	if c.GetBool("use_access_token") {
		respondRelayError(c, types.NewError(errors.New("暂不支持使用 access token"), types.ErrorCodeAccessDenied, types.ErrOptionWithSkipRetry()))
		return
	}

	meta, err := identityapp.BuildImageWorkspaceMetaFromRequest(c)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, nil, nil)
	if err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	userID := c.GetInt("id")
	if err := identityapp.WriteUserCacheToContext(c, userID); err != nil {
		respondRelayError(c, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()))
		return
	}

	tempToken := &identityschema.Token{
		UserId: userID,
		Name:   fmt.Sprintf("image-workspace-%s", relayInfo.UsingGroup),
		Group:  relayInfo.UsingGroup,
	}
	_ = middleware.SetupContextForToken(c, tempToken)

	c.Set(string(constant.ContextKeyImageWorkspaceCaptureResponse), true)
	relayRequest(c, types.RelayFormatOpenAIImage)

	if c.Writer.Status() >= http.StatusBadRequest {
		return
	}
	rawResponse, ok := c.Get(string(constant.ContextKeyImageWorkspaceResponseBody))
	if !ok {
		return
	}
	responseBody, ok := rawResponse.([]byte)
	if !ok || len(responseBody) == 0 {
		return
	}
	_, _ = identityapp.PersistImageWorkspaceResponse(c, meta, responseBody)
}

// RelayNotImplemented returns a standard OpenAI-style "not implemented" response.
func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

// RelayNotFound returns a standard OpenAI-style "invalid URL" response.
func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func upgradeRelayWebsocket(c *gin.Context, relayFormat types.RelayFormat) (*websocket.Conn, error) {
	if relayFormat != types.RelayFormatOpenAIRealtime {
		return nil, nil
	}
	ws, err := relayUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		relaycommon.WssError(c, nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
		return nil, err
	}
	return ws, nil
}

func respondRelayError(c *gin.Context, newAPIError *types.NewAPIError) {
	if newAPIError == nil {
		return
	}
	newAPIError.SanitizeDownstreamResponse()
	c.JSON(newAPIError.StatusCode, gin.H{
		"error": newAPIError.ToOpenAIError(),
	})
}
