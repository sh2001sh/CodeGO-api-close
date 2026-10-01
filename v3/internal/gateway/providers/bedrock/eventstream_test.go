package bedrock

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"sort"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func wireFrame(headers map[string]string, payload []byte) []byte {
	var h bytes.Buffer
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		h.WriteByte(byte(len(key)))
		h.WriteString(key)
		h.WriteByte(7)
		_ = binary.Write(&h, binary.BigEndian, uint16(len(headers[key])))
		h.WriteString(headers[key])
	}
	data := make([]byte, 12, 16+h.Len()+len(payload))
	binary.BigEndian.PutUint32(data, uint32(16+h.Len()+len(payload)))
	binary.BigEndian.PutUint32(data[4:], uint32(h.Len()))
	binary.BigEndian.PutUint32(data[8:], crc32.ChecksumIEEE(data[:8]))
	data = append(data, h.Bytes()...)
	data = append(data, payload...)
	checksum := crc32.ChecksumIEEE(data)
	data = binary.BigEndian.AppendUint32(data, checksum)
	return data
}

func chunkFrame(data string) []byte {
	payload, _ := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(data))})
	return wireFrame(map[string]string{":message-type": "event", ":event-type": "chunk"}, payload)
}

func decodeBinary(protocol gateway.Protocol, data []byte) gateway.EventStream {
	req := &gateway.Request{ID: "test", Protocol: protocol, Model: "client-model", Stream: true, Body: []byte(`{"stream_options":{"include_usage":true}}`)}
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(bytes.NewReader(data))}
	return (Provider{}).Decode(req, resp)
}

func claudeFrames() []byte {
	var data []byte
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	} {
		data = append(data, chunkFrame(event)...)
	}
	return data
}

func TestClaudeBinaryStreamUsageAndNativeEvents(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolOpenAIChat, gateway.ProtocolAnthropic} {
		stream := decodeBinary(protocol, claudeFrames())
		var usage *gateway.Usage
		var textBytes int
		done := false
		nativeStop := false
		for i := 0; i < 20; i++ {
			ev, err := stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			if ev.Usage != nil {
				usage = ev.Usage
			}
			textBytes += ev.TextBytes
			if protocol == gateway.ProtocolAnthropic && ev.Name == "message_stop" {
				nativeStop = true
			}
			if ev.Kind == gateway.EventDone {
				done = true
				break
			}
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
		if !done || usage == nil || usage.PromptTokens != 17 || usage.CompletionTokens != 7 || usage.CachedTokens != 3 || usage.CacheWriteTokens != 4 || textBytes != 5 {
			t.Fatalf("bad billing/terminal: done=%v usage=%+v bytes=%d", done, usage, textBytes)
		}
		if protocol == gateway.ProtocolAnthropic && !nativeStop {
			t.Fatal("native event name lost")
		}
	}
}

func TestBinaryStreamTruncationNeverCompletes(t *testing.T) {
	fixtures := [][]byte{claudeFrames()[:len(claudeFrames())-5], chunkFrame(`{"type":"message_start","message":{"id":"m","usage":{"input_tokens":2}}}`)}
	for _, data := range fixtures {
		stream := decodeBinary(gateway.ProtocolOpenAIChat, data)
		failed := false
		for i := 0; i < 20; i++ {
			ev, err := stream.Next()
			if err != nil {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("wrong truncation error: %v", err)
				}
				failed = true
				break
			}
			if ev.Kind == gateway.EventDone {
				t.Fatal("truncated stream reported completion")
			}
		}
		_ = stream.Close()
		if !failed {
			t.Fatal("truncation not detected")
		}
	}
}

func TestBinaryExceptionMapsRateLimit(t *testing.T) {
	data := wireFrame(map[string]string{":message-type": "exception", ":exception-type": "throttlingException"}, []byte(`{"message":"try later"}`))
	stream := decodeBinary(gateway.ProtocolOpenAIChat, data)
	defer func() { _ = stream.Close() }()
	ev, err := stream.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || ev.Err.Status != http.StatusTooManyRequests || ev.Err.Message != "try later" {
		t.Fatalf("exception lost: %+v %v", ev, err)
	}
}

func TestFrameChecksumsLengthsAndHeaders(t *testing.T) {
	valid := chunkFrame(`{"type":"message_stop"}`)
	preludeCRC := append([]byte(nil), valid...)
	preludeCRC[8] ^= 1
	messageCRC := append([]byte(nil), valid...)
	messageCRC[len(messageCRC)-1] ^= 1
	oversized := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(oversized, maxFrame+1)
	for name, data := range map[string][]byte{"prelude_crc": preludeCRC, "message_crc": messageCRC, "oversized": oversized, "partial": valid[:14]} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeFrame(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid frame accepted")
			}
		})
	}
	for _, data := range [][]byte{{0}, {1, 'a', 255}, {1, 'a', 7, 0, 4, 'x'}, {1, 'a', 7, 0, 0, 1, 'a', 7, 0, 0}} {
		if _, err := decodeHeaders(data); err == nil {
			t.Fatalf("invalid headers accepted: %v", data)
		}
	}
	if _, err := decodeHeaders([]byte{1, 'a', 0, 1, 'b', 1, 1, 'c', 2, 0, 1, 'd', 3, 0, 1, 1, 'e', 4, 0, 0, 0, 1}); err != nil {
		t.Fatalf("valid nonstring header rejected: %v", err)
	}
}

func TestChunkRejectsInvalidBase64AndNonobject(t *testing.T) {
	for _, wrapper := range []string{`{"bytes":"!!"}`, `{"bytes":"bnVsbA=="}`, `{"bytes":""}`, `{"bytes":"e2JhZA=="}`} {
		stream := decodeBinary(gateway.ProtocolAnthropic, wireFrame(map[string]string{":message-type": "event", ":event-type": "chunk"}, []byte(wrapper)))
		_, err := stream.Next()
		_ = stream.Close()
		if err == nil {
			t.Fatalf("invalid chunk accepted: %s", wrapper)
		}
	}
}

func TestInvokeJSONResponseAndNovaResponse(t *testing.T) {
	for _, data := range []string{
		`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":7}}`,
		`{"output":{"message":{"role":"assistant","content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":7}}`,
	} {
		req := &gateway.Request{Protocol: gateway.ProtocolOpenAIChat, Model: "m"}
		resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(data))}
		stream := (Provider{}).Decode(req, resp)
		ev, err := stream.Next()
		_ = stream.Close()
		if err != nil || ev.Kind != gateway.EventData || ev.Usage == nil || ev.Usage.PromptTokens != 10 || ev.Usage.CompletionTokens != 7 || gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != "hello" {
			t.Fatalf("response conversion failed: %+v %v", ev, err)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Fatal("mutated upstream header")
		}
	}
}
