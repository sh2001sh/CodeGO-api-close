package bedrock

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/tidwall/gjson"
)

const maxBody = 64 << 20
const maxFrame = 16 << 20

// eventBody lazily unwraps AWS binary frames into the existing Messages decoder.
// This preserves backpressure and does not introduce a pipe or reader goroutine.
type eventBody struct {
	body     io.ReadCloser
	reader   *bufio.Reader
	pending  *bytes.Reader
	nova     novaStream
	terminal bool
}

func newEventBody(body io.ReadCloser) *eventBody {
	return &eventBody{body: body, reader: bufio.NewReader(body)}
}

func (b *eventBody) Close() error { return b.body.Close() }

func (b *eventBody) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	for b.pending == nil || b.pending.Len() == 0 {
		if b.terminal {
			return 0, io.EOF
		}
		frame, err := decodeFrame(b.reader)
		if err != nil {
			return 0, fmt.Errorf("bedrock: invalid eventstream frame: %w", err)
		}
		kind := frame.headers[":message-type"]
		event := frame.headers[":event-type"]
		var data []byte
		if kind == "exception" || kind == "error" {
			code := frame.headers[":exception-type"]
			if code == "" {
				code = frame.headers[":error-code"]
			}
			message := gjson.GetBytes(frame.payload, "message").Str
			if message == "" {
				message = frame.headers[":error-message"]
			}
			if message == "" {
				message = "Bedrock stream failed"
			}
			data, err = json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": exceptionType(code), "message": message}})
			b.terminal = true
		} else if kind == "event" && event == "chunk" {
			var wrapper struct {
				Bytes string `json:"bytes"`
			}
			if err := json.Unmarshal(frame.payload, &wrapper); err != nil || wrapper.Bytes == "" {
				return 0, fmt.Errorf("bedrock: invalid chunk payload")
			}
			data, err = base64.StdEncoding.DecodeString(wrapper.Bytes)
			if err == nil && (!gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject()) {
				err = fmt.Errorf("bedrock: invalid chunk JSON")
			}
			if err == nil && !gjson.GetBytes(data, "type").Exists() {
				var events [][]byte
				events, err = b.nova.convert(data)
				if err == nil {
					b.pending = bytes.NewReader(sseEvents(events))
					continue
				}
			}
		} else {
			return 0, fmt.Errorf("bedrock: unsupported eventstream message")
		}
		if err != nil {
			return 0, err
		}
		b.pending = bytes.NewReader(sseEvents([][]byte{data}))
	}
	return b.pending.Read(dst)
}

func sseEvents(events [][]byte) []byte {
	var out bytes.Buffer
	for _, data := range events {
		out.WriteString("data: ")
		out.Write(data)
		out.WriteString("\n\n")
	}
	return out.Bytes()
}

func exceptionType(code string) string {
	switch code {
	case "throttlingException":
		return "rate_limit_error"
	case "validationException":
		return "invalid_request_error"
	case "modelTimeoutException":
		return "timeout_error"
	default:
		return "api_error"
	}
}

type responseBody struct {
	body   io.ReadCloser
	reader *bytes.Reader
	err    error
}

func (b *responseBody) Close() error { return b.body.Close() }

func (b *responseBody) Read(dst []byte) (int, error) {
	if b.reader == nil && b.err == nil {
		data, err := io.ReadAll(io.LimitReader(b.body, maxBody+1))
		if err == nil && len(data) > maxBody {
			err = fmt.Errorf("bedrock: response exceeds size limit")
		}
		if err == nil && gjson.GetBytes(data, "output.message").Exists() {
			data, err = novaResponse(data)
		}
		if err != nil {
			b.err = err
		} else {
			b.reader = bytes.NewReader(data)
		}
	}
	if b.err != nil {
		return 0, b.err
	}
	return b.reader.Read(dst)
}
