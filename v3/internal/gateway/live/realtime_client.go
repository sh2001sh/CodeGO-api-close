package live

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"golang.org/x/net/websocket"
)

func (h *Handler) connectRealtime(ctx context.Context, cfg *websocket.Config, req *gateway.Request, target gateway.Target) (*websocket.Conn, error) {
	client, err := h.upstreamClient(ctx, target)
	if err != nil {
		return nil, err
	}
	location := *cfg.Location
	location.Scheme = "http"
	if cfg.Location.Scheme == "wss" {
		location.Scheme = "https"
	}
	// net/http exposes a 101 upgrade body as a duplex connection. Running the
	// handshake through the selected client retains its proxy and TLS identity.
	handshake, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(30*time.Second, cancel)
	defer timer.Stop()
	upgrade := &realtimeUpgrade{ctx: handshake, cancel: cancel, client: client, location: &location, req: req, target: target}
	conn, err := websocket.NewClient(cfg, upgrade)
	if err != nil {
		_ = upgrade.Close()
		return nil, err
	}
	return conn, nil
}

type realtimeUpgrade struct {
	ctx      context.Context
	cancel   context.CancelFunc
	client   *http.Client
	location *url.URL
	req      *gateway.Request
	target   gateway.Target
	body     io.ReadWriteCloser
	prefix   *bytes.Reader
}

func (c *realtimeUpgrade) Write(data []byte) (int, error) {
	if c.body != nil {
		return c.body.Write(data)
	}
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return 0, err
	}
	request.URL = c.location
	request.RequestURI = ""
	request = request.WithContext(c.ctx)
	policy := c.target
	policy.ParamOverride, policy.Settings = nil, nil
	if err := gateway.ApplyUpstreamRequest(request, c.req, policy); err != nil {
		return 0, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return 0, err
	}
	if response.StatusCode != http.StatusSwitchingProtocols || gateway.MapUpstreamStatus(response.StatusCode, c.target) != http.StatusSwitchingProtocols {
		_ = response.Body.Close()
		return 0, errors.New("live: realtime upgrade rejected")
	}
	body, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		_ = response.Body.Close()
		return 0, errors.New("live: realtime upgrade is not duplex")
	}
	var prefix bytes.Buffer
	_, _ = fmt.Fprintf(&prefix, "HTTP/1.1 101 Switching Protocols\r\n")
	if err := response.Header.Write(&prefix); err != nil {
		_ = body.Close()
		return 0, err
	}
	prefix.WriteString("\r\n")
	c.body, c.prefix = body, bytes.NewReader(prefix.Bytes())
	return len(data), nil
}

func (c *realtimeUpgrade) Read(data []byte) (int, error) {
	if c.prefix != nil && c.prefix.Len() > 0 {
		return c.prefix.Read(data)
	}
	if c.body == nil {
		return 0, errors.New("live: realtime handshake was not sent")
	}
	return c.body.Read(data)
}

func (c *realtimeUpgrade) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	if c.body != nil {
		return c.body.Close()
	}
	return nil
}
