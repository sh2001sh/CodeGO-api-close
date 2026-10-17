package taskhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/constant"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	"github.com/sh2001sh/new-api/types"

	"github.com/gin-gonic/gin"
)

var marketplaceHTTPClient = platformhttpx.GetMarketplaceHTTPClientWithResponseHeaderTimeout

// DoUpstreamRequest shares the mandatory marketplace policy between task
// submission and the later operation polling performed by workflow workers.
func DoUpstreamRequest(req *http.Request, marketplace bool, proxy string) (*http.Response, error) {
	var client *http.Client
	var err error
	if marketplace {
		client = marketplaceHTTPClient(time.Duration(platformconfig.RelayResponseHeaderTimeout) * time.Second)
	} else {
		client, err = platformhttpx.GetHTTPClientWithProxy(proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	}
	resp, err := client.Do(req)
	if err != nil && marketplace {
		if req.Context().Err() != nil {
			return resp, req.Context().Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return resp, context.DeadlineExceeded
		}
		if errors.Is(err, platformhttpx.ErrMarketplaceUpstreamAddress) {
			return resp, platformhttpx.ErrMarketplaceUpstreamAddress
		}
		return resp, errors.New("marketplace task upstream request failed")
	}
	return resp, err
}

type RequestAdaptor interface {
	BuildRequestURL(info *relaycommon.RelayInfo) (string, error)
	BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error
}

func applyUpstreamContentLength(req *http.Request, info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	if info.UpstreamRequestBodySize > 0 && req.ContentLength <= 0 {
		req.ContentLength = info.UpstreamRequestBodySize
	}
}

func doRequest(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) (*http.Response, error) {
	resp, err := DoUpstreamRequest(req, info.UsesMarketplaceEgress(), info.ChannelSetting.Proxy)
	if err != nil {
		logger.LogError(c, "do request failed: "+err.Error())
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithHideErrMsg("upstream error: do request failed"))
	}
	if resp == nil {
		return nil, fmt.Errorf("resp is nil")
	}

	if upID := resp.Header.Get(constant.RequestIdKey); upID != "" {
		c.Set(constant.UpstreamRequestIdKey, upID)
	}

	if req.Body != nil {
		_ = req.Body.Close()
	}
	_ = c.Request.Body.Close()
	return resp, nil
}

func DoTaskAPIRequest(a RequestAdaptor, c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	fullRequestURL, err := a.BuildRequestURL(info)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, fullRequestURL, requestBody)
	if err != nil {
		if info.UsesMarketplaceEgress() {
			return nil, errors.New("invalid marketplace task upstream request")
		}
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	applyUpstreamContentLength(req, info)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(requestBody), nil
	}

	if err := a.BuildRequestHeader(c, req, info); err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}
	resp, err := doRequest(c, req, info)
	if err != nil {
		return nil, fmt.Errorf("do request failed: %w", err)
	}
	return resp, nil
}
