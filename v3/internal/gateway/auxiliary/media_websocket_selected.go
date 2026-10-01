package auxiliary

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// The selected credential client also governs native speech upgrades.
func connectMediaWebsocket(ctx context.Context, cfg *websocket.Config, request *http.Request) (*websocket.Conn, *mediaUpgrade, error) {
	client, ok := ctx.Value(clientKey{}).(*http.Client)
	if !ok || client == nil || client.Transport == nil {
		return nil, nil, errors.New("selected speech client is unavailable")
	}
	handshake, cancel := context.WithCancel(ctx)
	selected := *client
	selected.CheckRedirect = noRedirect
	upgrade := &mediaUpgrade{ctx: handshake, cancel: cancel, client: &selected, request: request}
	stop := context.AfterFunc(handshake, func() { _ = upgrade.Close() })
	defer stop()
	timer := time.AfterFunc(30*time.Second, func() { _ = upgrade.Close() })
	defer timer.Stop()
	conn, err := websocket.NewClient(cfg, upgrade)
	if err != nil {
		timedOut := ctx.Err() == nil && handshake.Err() != nil
		_ = upgrade.Close()
		if timedOut {
			return nil, nil, context.DeadlineExceeded
		}
		return nil, nil, err
	}
	return conn, upgrade, nil
}

type mediaUpgrade struct {
	ctx     context.Context
	cancel  context.CancelFunc
	client  *http.Client
	request *http.Request
	mu      sync.Mutex
	closed  bool
	body    io.ReadWriteCloser
	prefix  *bytes.Reader
}

func (c *mediaUpgrade) Write(data []byte) (int, error) {
	c.mu.Lock()
	closed, body := c.closed, c.body
	c.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	if body != nil {
		return body.Write(data)
	}
	body, prefix, err := c.performUpgrade(data)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = body.Close()
		return 0, io.ErrClosedPipe
	}
	c.body, c.prefix = body, prefix
	c.mu.Unlock()
	return len(data), nil
}

// performUpgrade replays the client's buffered HTTP upgrade request against
// the selected channel transport and validates the resulting duplex
// response, returning the connection body and the synthesized status-line
// prefix that Read must replay before any upstream bytes.
func (c *mediaUpgrade) performUpgrade(data []byte) (io.ReadWriteCloser, *bytes.Reader, error) {
	request, err := c.buildUpgradeRequest(data)
	if err != nil {
		return nil, nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	return acceptUpgradeResponse(response)
}

// buildUpgradeRequest re-targets the client's parsed HTTP/1.1 upgrade
// request at the upstream channel host, dropping body/transfer-encoding
// fields that have no meaning for the handshake replay.
func (c *mediaUpgrade) buildUpgradeRequest(data []byte) (*http.Request, error) {
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, err
	}
	location := *c.request.URL
	location.Scheme = "http"
	if c.request.URL.Scheme == "wss" {
		location.Scheme = "https"
	}
	request.URL, request.RequestURI, request.Host = &location, "", c.request.Host
	request = request.WithContext(c.ctx)
	for name, values := range c.request.Header {
		request.Header[name] = append([]string(nil), values...)
	}
	request.Body, request.GetBody, request.ContentLength = nil, nil, 0
	request.Trailer, request.TransferEncoding = nil, nil
	request.Header.Del("Content-Length")
	request.Header.Del("Transfer-Encoding")
	return request, nil
}

// acceptUpgradeResponse validates that the upstream response is a duplex
// 101 Switching Protocols and synthesizes the status-line/header prefix the
// websocket client expects to read back.
func acceptUpgradeResponse(response *http.Response) (io.ReadWriteCloser, *bytes.Reader, error) {
	if response == nil || response.Body == nil {
		return nil, nil, errors.New("speech upgrade response is missing")
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = response.Body.Close()
		return nil, nil, errors.New("speech upgrade rejected")
	}
	body, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		_ = response.Body.Close()
		return nil, nil, errors.New("speech upgrade is not duplex")
	}
	var prefix bytes.Buffer
	_, _ = fmt.Fprint(&prefix, "HTTP/1.1 101 Switching Protocols\r\n")
	if err := response.Header.Write(&prefix); err != nil {
		_ = body.Close()
		return nil, nil, err
	}
	prefix.WriteString("\r\n")
	return body, bytes.NewReader(prefix.Bytes()), nil
}

func (c *mediaUpgrade) Read(data []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if c.prefix != nil && c.prefix.Len() > 0 {
		n, err := c.prefix.Read(data)
		c.mu.Unlock()
		return n, err
	}
	body := c.body
	c.mu.Unlock()
	if body == nil {
		return 0, errors.New("speech handshake was not sent")
	}
	return body.Read(data)
}

// Close the duplex body first; WebSocket Close writes a control frame that
// could block cancellation behind a stalled native write.
func (c *mediaUpgrade) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	body := c.body
	c.mu.Unlock()
	c.cancel()
	if body != nil {
		return body.Close()
	}
	return nil
}
