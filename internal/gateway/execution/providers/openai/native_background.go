package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/internal/gateway/execution/providers/synchttp"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

const nativeBackgroundResumeMaxDelay = 2 * time.Second

type nativeBackgroundEvent struct {
	Type           string `json:"type"`
	SequenceNumber int64  `json:"sequence_number"`
	Response       struct {
		ID string `json:"id"`
	} `json:"response"`
}

type nativeBackgroundBody struct {
	ctx         context.Context
	current     io.ReadCloser
	resume      func(string, int64) (*http.Response, error)
	onCursor    func(string, int64)
	responseID  string
	sequence    int64
	terminal    bool
	closed      bool
	pendingErr  error
	parseBuffer []byte
	retryDelay  time.Duration
}

func doNativeBackgroundRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, rawBody []byte) (*http.Response, error) {
	endpoint, err := a.GetRequestURL(info)
	if err != nil {
		return nil, err
	}
	responseID := c.GetString(string(constant.ContextKeyBackgroundResumeID))
	sequence := c.GetInt64(string(constant.ContextKeyBackgroundResumeCursor))
	var response *http.Response
	if responseID == "" {
		response, err = synchttp.DoAPIRequest(a, c, info, bytes.NewReader(rawBody))
	} else {
		response, err = synchttp.DoAPIRequestAt(a, c, info, http.MethodGet, nativeBackgroundResumeURL(endpoint, responseID, sequence), nil)
	}
	if err != nil || response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, err
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return response, nil
	}
	jobID := c.GetString(constant.RequestIdKey)
	response.Body = &nativeBackgroundBody{
		ctx: response.Request.Context(), current: response.Body,
		responseID: responseID, sequence: sequence, retryDelay: 250 * time.Millisecond,
		resume: func(id string, cursor int64) (*http.Response, error) {
			return resumeNativeBackground(a, c, info, endpoint, id, cursor)
		},
		onCursor: func(id string, cursor int64) {
			if jobID != "" {
				_ = gatewaystore.UpdateResponsesBackgroundUpstreamCursor(jobID, id, cursor)
			}
		},
	}
	return response, nil
}

func (body *nativeBackgroundBody) Read(buffer []byte) (int, error) {
	for {
		if body.closed {
			return 0, io.ErrClosedPipe
		}
		if body.pendingErr != nil {
			err := body.pendingErr
			body.pendingErr = nil
			if resumeErr := body.handleSourceEnd(err); resumeErr != nil {
				return 0, resumeErr
			}
			continue
		}
		n, err := body.current.Read(buffer)
		if n > 0 {
			body.observe(buffer[:n])
			body.pendingErr = err
			return n, nil
		}
		if err == nil {
			continue
		}
		if resumeErr := body.handleSourceEnd(err); resumeErr != nil {
			return 0, resumeErr
		}
	}
}

func (body *nativeBackgroundBody) Close() error {
	body.closed = true
	if body.current == nil {
		return nil
	}
	return body.current.Close()
}

func (body *nativeBackgroundBody) handleSourceEnd(sourceErr error) error {
	if body.current != nil {
		_ = body.current.Close()
	}
	if body.terminal || body.closed {
		return sourceErr
	}
	if strings.TrimSpace(body.responseID) == "" {
		if errors.Is(sourceErr, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		return sourceErr
	}
	response, err := body.resume(body.responseID, body.sequence)
	if err != nil {
		return err
	}
	body.current = response.Body
	return nil
}

func (body *nativeBackgroundBody) observe(chunk []byte) {
	body.parseBuffer = append(body.parseBuffer, chunk...)
	for {
		index, size := nativeBackgroundEventBoundary(body.parseBuffer)
		if index < 0 {
			return
		}
		frame := body.parseBuffer[:index]
		body.parseBuffer = body.parseBuffer[index+size:]
		for _, line := range bytes.Split(frame, []byte{'\n'}) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			var event nativeBackgroundEvent
			if platformencoding.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &event) != nil {
				continue
			}
			if event.Response.ID != "" {
				body.responseID = event.Response.ID
			}
			if event.SequenceNumber >= body.sequence {
				body.sequence = event.SequenceNumber
			}
			if body.responseID != "" && body.onCursor != nil {
				body.onCursor(body.responseID, body.sequence)
			}
			switch event.Type {
			case "response.completed", "response.failed", "response.incomplete", "response.cancelled", "error":
				body.terminal = true
			}
		}
	}
}

func resumeNativeBackground(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, endpoint, responseID string, sequence int64) (*http.Response, error) {
	delay := 250 * time.Millisecond
	for {
		response, err := synchttp.DoAPIRequestAt(a, c, info, http.MethodGet, nativeBackgroundResumeURL(endpoint, responseID, sequence), nil)
		if err == nil && response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		if response != nil {
			status := response.StatusCode
			_ = response.Body.Close()
			if status != http.StatusNotFound && status != http.StatusConflict && status != http.StatusTooManyRequests && status < http.StatusInternalServerError {
				return nil, fmt.Errorf("background resume rejected with status %d", status)
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-c.Request.Context().Done():
			timer.Stop()
			return nil, c.Request.Context().Err()
		case <-timer.C:
		}
		if delay < nativeBackgroundResumeMaxDelay {
			delay *= 2
			if delay > nativeBackgroundResumeMaxDelay {
				delay = nativeBackgroundResumeMaxDelay
			}
		}
	}
}

func nativeBackgroundResumeURL(endpoint, responseID string, sequence int64) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + url.PathEscape(responseID)
	query := parsed.Query()
	query.Set("stream", "true")
	query.Set("starting_after", strconv.FormatInt(sequence, 10))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func nativeBackgroundEventBoundary(data []byte) (int, int) {
	lf := bytes.Index(data, []byte("\n\n"))
	crlf := bytes.Index(data, []byte("\r\n\r\n"))
	switch {
	case lf < 0:
		return crlf, 4
	case crlf < 0 || lf < crlf:
		return lf, 2
	default:
		return crlf, 4
	}
}
