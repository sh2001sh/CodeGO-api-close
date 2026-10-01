package providers_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"strings"
	"testing"
)

func bedrockReplayFixture(id string) replayFixture {
	f := anthropicReplayFixture(id)
	f.model, f.secret = "anthropic.claude-sonnet-4-20250514-v1:0", "test-access|test-signing-secret|us-east-1"
	f.path, f.contentType = "/model/"+f.model+"/invoke-with-response-stream", "application/vnd.amazon.eventstream"
	f.preamble = bedrockChunk(`{"type":"message_start","message":{"id":"m1","role":"assistant","content":[]}}`)
	f.preambleUsage = bedrockChunk(`{"type":"message_start","message":{"id":"m1","role":"assistant","content":[],"usage":{"input_tokens":10}}}`)
	f.content = bedrockChunk(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`)
	stop := bedrockChunk(`{"type":"message_stop"}`)
	f.finish = bedrockChunk(`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`) + stop
	f.finishUsage = bedrockChunk(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`) + stop
	f.empty = f.preamble + f.finish
	f.check = func(t *testing.T, r *http.Request, b []byte) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-access/") || !strings.Contains(r.Header.Get("Authorization"), "/us-east-1/bedrock/aws4_request") {
			t.Errorf("Bedrock SigV4 scope lost")
		}
		assertHeader(t, r, "Accept", "application/vnd.amazon.eventstream")
		assertHeader(t, r, "X-Amzn-Bedrock-Accept", "application/json")
		assertJSON(t, b, "anthropic_version", "bedrock-2023-05-31")
		assertJSON(t, b, "messages.0.content.0.text", "hello")
		forbidJSON(t, b, "model", "stream", "stream_options")
	}
	return f
}

func bedrockNovaReplayFixture() replayFixture {
	f := bedrockReplayFixture("bedrock")
	f.name, f.model = "bedrock-nova", "amazon.nova-pro-v1:0"
	f.path = "/model/" + f.model + "/invoke-with-response-stream"
	f.preamble = bedrockChunk(`{"messageStart":{"role":"assistant"}}`)
	f.preambleUsage = f.preamble
	f.content = bedrockChunk(`{"contentBlockDelta":{"contentBlockIndex":0,"delta":{"text":"hello"}}}`)
	stop := bedrockChunk(`{"contentBlockStop":{"contentBlockIndex":0}}`) + bedrockChunk(`{"messageStop":{"stopReason":"end_turn"}}`)
	f.finish = stop + bedrockChunk(`{"metadata":{}}`)
	f.finishUsage = stop + bedrockChunk(`{"metadata":{"usage":{"inputTokens":10,"outputTokens":3}}}`)
	f.empty = f.preamble + bedrockChunk(`{"messageStop":{"stopReason":"end_turn"}}`) + bedrockChunk(`{"metadata":{}}`)
	f.check = func(t *testing.T, r *http.Request, b []byte) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-access/") {
			t.Errorf("Nova SigV4 lost")
		}
		assertJSON(t, b, "schemaVersion", "messages-v1")
		assertJSON(t, b, "messages.0.content.0.text", "hello")
		forbidJSON(t, b, "model", "anthropic_version", "stream_options")
	}
	return f
}

// AWS wire frames include both checksums and the Base64 JSON envelope; the
// fixture never substitutes SSE for the native binary transport.
func bedrockChunk(data string) string {
	var headers bytes.Buffer
	for _, h := range [][2]string{{":message-type", "event"}, {":event-type", "chunk"}} {
		headers.WriteByte(byte(len(h[0])))
		headers.WriteString(h[0])
		headers.WriteByte(7)
		_ = binary.Write(&headers, binary.BigEndian, uint16(len(h[1])))
		headers.WriteString(h[1])
	}
	payload, _ := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(data))})
	frame := make([]byte, 12)
	binary.BigEndian.PutUint32(frame, uint32(16+headers.Len()+len(payload)))
	binary.BigEndian.PutUint32(frame[4:], uint32(headers.Len()))
	binary.BigEndian.PutUint32(frame[8:], crc32.ChecksumIEEE(frame[:8]))
	frame = append(frame, headers.Bytes()...)
	frame = append(frame, payload...)
	frame = binary.BigEndian.AppendUint32(frame, crc32.ChecksumIEEE(frame))
	return string(frame)
}
