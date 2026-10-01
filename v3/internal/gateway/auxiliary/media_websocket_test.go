package auxiliary

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func volcMediaAudioFrame(sequence int32, data []byte) []byte {
	flags := byte(1)
	if sequence < 0 {
		flags = 3
	}
	frame := make([]byte, 12, len(data)+12)
	copy(frame, []byte{0x11, 0xb0 | flags, 0, 0})
	binary.BigEndian.PutUint32(frame[4:], uint32(sequence))
	binary.BigEndian.PutUint32(frame[8:], uint32(len(data)))
	return append(frame, data...)
}

func localVolcMediaRequest(t *testing.T, ctx context.Context, address string) (*http.Request, *gateway.Request) {
	t.Helper()
	req := &gateway.Request{Model: "tts", Body: []byte(`{"model":"tts","input":"Hello","voice":"alloy","response_format":"wav"}`)}
	r, err := mediaAdapters()["volcengine"].Build(ctx, req, gateway.Target{Secret: "app|native-token"}, Input{Operation: Speech})
	if err != nil {
		t.Fatal(err)
	}
	if r.URL.Scheme != "wss" || r.URL.Host != "openspeech.bytedance.com" || r.URL.Path != "/api/v1/tts/ws_binary" {
		t.Fatalf("native URL %s", r.URL)
	}
	local, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	r.URL.Scheme, r.URL.Host = "ws", local.Host
	return r, req
}

func TestVolcMediaWebsocketNativeFramingAndFinalSequence(t *testing.T) {
	server := httptest.NewServer(websocket.Server{Handshake: func(cfg *websocket.Config, r *http.Request) error {
		if r.URL.Path != "/api/v1/tts/ws_binary" || r.Header.Get("Authorization") != "Bearer;native-token" {
			t.Errorf("handshake path/auth %s", r.URL.Path)
		}
		return nil
	}, Handler: func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var frame []byte
		if err := websocket.Message.Receive(conn, &frame); err != nil {
			t.Error(err)
			return
		}
		if len(frame) < 8 || !bytes.Equal(frame[:4], []byte{0x11, 0x10, 0x10, 0}) || binary.BigEndian.Uint32(frame[4:8]) != uint32(len(frame)-8) {
			t.Errorf("invalid client framing %v", frame)
			return
		}
		body := frame[8:]
		if gjson.GetBytes(body, "request.operation").String() != "submit" || gjson.GetBytes(body, "app.appid").String() != "app" || gjson.GetBytes(body, "request.model").String() != "tts" {
			t.Errorf("native request %s", body)
		}
		_ = websocket.Message.Send(conn, []byte{0x11, 0xb0, 0, 0})
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(1, []byte{0, 1}))
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(-2, []byte{2, 255}))
	}})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, req := localVolcMediaRequest(t, ctx, server.URL)
	upstream, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, server.Client()), r)
	if err != nil {
		t.Fatal(err)
	}
	response, err := mediaAdapters()["volcengine"].Decode(ctx, req, gateway.Target{}, Input{Operation: Speech}, upstream)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.Body, []byte{0, 1, 2, 255}) || response.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("response %+v", response)
	}
}

func TestVolcMediaWebsocketCancellationClosesSocket(t *testing.T) {
	started := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var frame []byte
		_ = websocket.Message.Receive(conn, &frame)
		close(started)
		_ = websocket.Message.Receive(conn, &frame)
		close(closed)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := localVolcMediaRequest(t, ctx, server.URL)
	finished := make(chan error, 1)
	go func() {
		_, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, server.Client()), r)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("websocket did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("websocket ignored cancellation")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("websocket remained open")
	}
}

func TestVolcMediaWebsocketNativeErrorDoesNotLeakCredential(t *testing.T) {
	server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var data []byte
		_ = websocket.Message.Receive(conn, &data)
		payload := []byte("native-token")
		frame := make([]byte, 12)
		copy(frame, []byte{0x11, 0xf0, 0x10, 0})
		binary.BigEndian.PutUint32(frame[4:], 1001)
		binary.BigEndian.PutUint32(frame[8:], uint32(len(payload)))
		_ = websocket.Message.Send(conn, append(frame, payload...))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, _ := localVolcMediaRequest(t, ctx, server.URL)
	_, err := mediaWebsocketRoundTrip(context.WithValue(ctx, clientKey{}, server.Client()), r)
	if err == nil || strings.Contains(err.Error(), "native-token") {
		t.Fatalf("unsafe error %v", err)
	}
}

func TestVolcMediaFrameCompressionAndInvalidLengths(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte{0, 1, 2, 255})
	_ = writer.Close()
	frame := volcMediaAudioFrame(-1, compressed.Bytes())
	frame[2] = 1
	payload, done, err := parseVolcMediaFrame(frame)
	if err != nil || !done || !bytes.Equal(payload, []byte{0, 1, 2, 255}) {
		t.Fatalf("gzip frame %v %v %v", payload, done, err)
	}
	cases := [][]byte{nil, {0x11}, {0x01, 0xb0, 0, 0}, {0x10, 0xb0, 0, 0}, {0x11, 0xb4, 0, 0}, volcMediaAudioFrame(-1, []byte{1, 2})[:13]}
	oversize := volcMediaAudioFrame(1, nil)
	binary.BigEndian.PutUint32(oversize[8:], 0xffffffff)
	cases = append(cases, oversize)
	for _, bad := range cases {
		if _, _, err := parseVolcMediaFrame(bad); err == nil {
			t.Errorf("bad frame accepted %v", bad)
		}
	}
}

func TestVolcMediaWebsocketDisconnectBeforeFinalSequenceFails(t *testing.T) {
	server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		var frame []byte
		_ = websocket.Message.Receive(conn, &frame)
		_ = websocket.Message.Send(conn, volcMediaAudioFrame(1, []byte{1, 2}))
	}))
	defer server.Close()
	r, _ := localVolcMediaRequest(t, context.Background(), server.URL)
	response, err := mediaWebsocketRoundTrip(context.WithValue(context.Background(), clientKey{}, server.Client()), r)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("truncated audio accepted")
	}
}
