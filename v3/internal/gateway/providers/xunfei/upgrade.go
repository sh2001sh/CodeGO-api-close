package xunfei

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"golang.org/x/net/websocket"
)

var _ gateway.TransportProvider = Provider{}

func (Provider) UpstreamTransport(_ *gateway.Request, fallback http.RoundTripper) http.RoundTripper {
	return nativeTransport{fallback: fallback}
}

type nativeTransport struct{ fallback http.RoundTripper }

func (t nativeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nativeRoundTrip(req, t.connect)
}

func (t nativeTransport) connect(req *http.Request, config *websocket.Config) (*websocket.Conn, io.Closer, error) {
	if t.fallback == nil {
		return nil, nil, errors.New("xunfei: selected upstream transport is unavailable")
	}
	handshake, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(5*time.Second, cancel)
	defer timer.Stop()
	upgrade := &sparkUpgrade{ctx: handshake, cancel: cancel, fallback: t.fallback, request: req}
	conn, err := websocket.NewClient(config, upgrade)
	if err != nil {
		timedOut := req.Context().Err() == nil && handshake.Err() != nil
		_ = upgrade.Close()
		if timedOut {
			err = context.DeadlineExceeded
		}
		return nil, nil, err
	}
	// Stop only the handshake timer. Its context remains tied to the original
	// request until the duplex connection is closed; canceling now kills it.
	return conn, upgrade, nil
}

type sparkUpgrade struct {
	ctx      context.Context
	cancel   context.CancelFunc
	fallback http.RoundTripper
	request  *http.Request
	body     io.ReadWriteCloser
	prefix   *bytes.Reader
}

func (c *sparkUpgrade) Write(data []byte) (int, error) {
	if c.body != nil {
		return c.body.Write(data)
	}
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return 0, err
	}
	location := *c.request.URL
	request.URL, request.RequestURI = &location, ""
	request = request.WithContext(c.ctx)
	// Keep the final upstream header overrides, but send the native JSON only
	// as a WebSocket frame after the generated GET handshake has succeeded.
	for key, values := range c.request.Header {
		request.Header[key] = append([]string(nil), values...)
	}
	request.Host = c.request.Host
	request.Body, request.GetBody, request.ContentLength = nil, nil, 0
	request.Trailer, request.TransferEncoding = nil, nil
	request.Header.Del("Content-Length")
	request.Header.Del("Transfer-Encoding")
	response, err := c.fallback.RoundTrip(request)
	if err != nil {
		return 0, err
	}
	if response == nil || response.Body == nil {
		return 0, errors.New("xunfei: WebSocket upgrade response is missing")
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = response.Body.Close()
		return 0, errors.New("xunfei: WebSocket upgrade rejected")
	}
	body, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		_ = response.Body.Close()
		return 0, errors.New("xunfei: WebSocket upgrade is not duplex")
	}
	var prefix bytes.Buffer
	_, _ = fmt.Fprint(&prefix, "HTTP/1.1 101 Switching Protocols\r\n")
	if err := response.Header.Write(&prefix); err != nil {
		_ = body.Close()
		return 0, err
	}
	prefix.WriteString("\r\n")
	c.body, c.prefix = body, bytes.NewReader(prefix.Bytes())
	return len(data), nil
}

func (c *sparkUpgrade) Read(data []byte) (int, error) {
	if c.prefix != nil && c.prefix.Len() > 0 {
		return c.prefix.Read(data)
	}
	if c.body == nil {
		return 0, errors.New("xunfei: WebSocket handshake was not sent")
	}
	return c.body.Read(data)
}

func (c *sparkUpgrade) Close() error {
	c.cancel()
	if c.body != nil {
		return c.body.Close()
	}
	return nil
}
