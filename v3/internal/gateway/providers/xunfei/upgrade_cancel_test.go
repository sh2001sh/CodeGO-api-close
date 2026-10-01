package xunfei

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestSelectedCancellationInterruptsBlockedNativeFrameWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, out := transportRequest(t, ctx, "https://8.8.8.8/v3.5/chat", true)
	body := &blockedUpgradeBody{written: make(chan struct{}), closed: make(chan struct{})}
	fallback := selectedRoundTripper(func(upgrade *http.Request) (*http.Response, error) {
		accept := sha1.Sum([]byte(upgrade.Header.Get("Sec-Websocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		return &http.Response{StatusCode: http.StatusSwitchingProtocols, Header: http.Header{
			"Connection": {"Upgrade"}, "Upgrade": {"websocket"}, "Sec-Websocket-Accept": {base64.StdEncoding.EncodeToString(accept[:])},
		}, Body: body}, nil
	})
	result := make(chan error, 1)
	go func() {
		_, err := (Provider{}).UpstreamTransport(req, fallback).RoundTrip(out)
		result <- err
	}()
	select {
	case <-body.written:
	case <-time.After(time.Second):
		t.Fatal("native frame was not written through selected duplex body")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("blocked selected frame lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WebSocket control close blocked behind pending native frame write")
	}
}

type blockedUpgradeBody struct {
	written, closed      chan struct{}
	writeOnce, closeOnce sync.Once
}

func (b *blockedUpgradeBody) Write([]byte) (int, error) {
	b.writeOnce.Do(func() { close(b.written) })
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockedUpgradeBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.EOF
}

func (b *blockedUpgradeBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}
