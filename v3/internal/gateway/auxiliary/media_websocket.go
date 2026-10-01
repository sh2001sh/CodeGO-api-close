package auxiliary

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

const maxMediaAudio = 32 << 20

// mediaWebsocketRoundTrip adapts the legacy Volcengine binary TTS transport.
// No bytes are delivered to the caller until the native final sequence arrives.
func mediaWebsocketRoundTrip(ctx context.Context, request *http.Request) (*http.Response, error) {
	conn, transport, err := dialMediaWebsocket(ctx, request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transport.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = transport.Close() })
	defer stop()
	if err := sendMediaWebsocketRequest(ctx, conn, transport, request); err != nil {
		return nil, err
	}
	audio, err := receiveMediaWebsocketAudio(ctx, conn, transport)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"code": 3000, "data": base64.StdEncoding.EncodeToString(audio)})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
}

// dialMediaWebsocket establishes the legacy Volcengine websocket connection
// and configures its max payload size.
func dialMediaWebsocket(ctx context.Context, request *http.Request) (*websocket.Conn, io.Closer, error) {
	origin := *request.URL
	origin.Scheme = "https"
	if request.URL.Scheme == "ws" {
		origin.Scheme = "http"
	}
	cfg, err := websocket.NewConfig(request.URL.String(), origin.Scheme+"://"+origin.Host)
	if err != nil {
		return nil, nil, mediaError("volcengine", "invalid_websocket_url")
	}
	cfg.Header = request.Header.Clone()
	conn, transport, err := connectMediaWebsocket(ctx, cfg, request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, context.DeadlineExceeded
		}
		return nil, nil, mediaError("volcengine", "websocket_connect_failed")
	}
	conn.MaxPayloadBytes = maxMediaAudio
	return conn, transport, nil
}

// sendMediaWebsocketRequest reads the client's TTS request body and sends it
// to the upstream connection as a single binary frame.
func sendMediaWebsocketRequest(ctx context.Context, conn *websocket.Conn, transport io.Closer, request *http.Request) error {
	data, err := io.ReadAll(io.LimitReader(request.Body, (1<<20)+1))
	_ = request.Body.Close()
	if err != nil || len(data) > 1<<20 {
		return mediaError("volcengine", "invalid_tts_request")
	}
	frame := make([]byte, 8, len(data)+8)
	copy(frame, []byte{0x11, 0x10, 0x10, 0})
	binary.BigEndian.PutUint32(frame[4:], uint32(len(data)))
	frame = append(frame, data...)
	writeTimer := time.AfterFunc(30*time.Second, func() { _ = transport.Close() })
	err = websocket.Message.Send(conn, frame)
	writeTimer.Stop()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return mediaError("volcengine", "websocket_send_failed")
	}
	return nil
}

// receiveMediaWebsocketAudio reads binary frames from the upstream
// connection until the final sequence arrives, accumulating decoded audio.
func receiveMediaWebsocketAudio(ctx context.Context, conn *websocket.Conn, transport io.Closer) ([]byte, error) {
	var audio []byte
	for count := 0; count < 65536; count++ {
		var data []byte
		readTimer := time.AfterFunc(30*time.Second, func() { _ = transport.Close() })
		err := websocket.Message.Receive(conn, &data)
		readTimer.Stop()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, mediaError("volcengine", "websocket_truncated")
		}
		chunk, done, err := parseVolcMediaFrame(data)
		if err != nil {
			return nil, err
		}
		if len(chunk) > maxMediaAudio-len(audio) {
			return nil, mediaError("volcengine", "audio_too_large")
		}
		audio = append(audio, chunk...)
		if !done {
			continue
		}
		if len(audio) == 0 {
			return nil, mediaError("volcengine", "empty_audio")
		}
		return audio, nil
	}
	return nil, mediaError("volcengine", "too_many_audio_frames")
}

func volcMediaFrameFail() ([]byte, bool, error) {
	return nil, false, mediaError("volcengine", "invalid_audio_frame")
}

func parseVolcMediaFrame(data []byte) ([]byte, bool, error) {
	header, kind, flags, compression, ok := volcMediaFrameHeader(data)
	if !ok {
		return volcMediaFrameFail()
	}
	if kind == 15 {
		return nil, false, mediaError("volcengine", "volcengine_tts_error")
	}
	if kind != 11 && kind != 9 && kind != 12 {
		return volcMediaFrameFail()
	}
	// Acknowledgements can carry no payload.
	if header == len(data) && flags == 0 {
		return nil, false, nil
	}
	payload, sequence, ok := volcMediaFramePayload(data, header, flags, compression)
	if !ok {
		return volcMediaFrameFail()
	}
	done := sequence < 0 || flags == 3
	if kind != 11 {
		if err := volcMediaFrameStatus(payload); err != nil {
			return nil, false, err
		}
		return nil, done, nil
	}
	return payload, done, nil
}

// volcMediaFrameHeader parses and validates the fixed frame header, returning
// the header length (in bytes), message kind, flags, and compression method.
func volcMediaFrameHeader(data []byte) (header int, kind, flags, compression byte, ok bool) {
	if len(data) < 4 || data[0]>>4 != 1 {
		return 0, 0, 0, 0, false
	}
	header = int(data[0]&0x0f) * 4
	if header < 4 || header > len(data) {
		return 0, 0, 0, 0, false
	}
	kind, flags, compression = data[1]>>4, data[1]&15, data[2]&15
	if flags != 0 && flags != 1 && flags != 3 {
		return 0, 0, 0, 0, false
	}
	return header, kind, flags, compression, true
}

// volcMediaFramePayload extracts and (if needed) decompresses the frame
// payload, returning the sequence number parsed from the optional sequence
// field.
func volcMediaFramePayload(data []byte, header int, flags, compression byte) (payload []byte, sequence int32, ok bool) {
	offset := header
	if flags == 1 || flags == 3 {
		if offset+4 > len(data) {
			return nil, 0, false
		}
		sequence = int32(binary.BigEndian.Uint32(data[offset:]))
		offset += 4
	}
	if offset+4 > len(data) {
		return nil, 0, false
	}
	size := uint64(binary.BigEndian.Uint32(data[offset:]))
	offset += 4
	if size != uint64(len(data)-offset) || size > maxMediaAudio {
		return nil, 0, false
	}
	payload = data[offset:]
	switch compression {
	case 0:
		return payload, sequence, true
	case 1:
		reader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, 0, false
		}
		decoded, err := io.ReadAll(io.LimitReader(reader, maxMediaAudio+1))
		_ = reader.Close()
		if err != nil || len(decoded) > maxMediaAudio {
			return nil, 0, false
		}
		return decoded, sequence, true
	default:
		return nil, 0, false
	}
}

// volcMediaFrameStatus validates a non-audio frame's JSON payload and
// surfaces an upstream error if it carries a nonzero status code.
func volcMediaFrameStatus(payload []byte) error {
	if len(payload) > 0 && !json.Valid(payload) {
		return mediaError("volcengine", "invalid_audio_frame")
	}
	if strings.Contains(string(payload), `"code"`) {
		var status struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(payload, &status) != nil || (status.Code != 0 && status.Code != 3000) {
			return mediaError("volcengine", "volcengine_tts_error")
		}
	}
	return nil
}
