package responses

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBuiltInToolMeterDeduplicatesLifecycleAndTerminalSnapshots(t *testing.T) {
	item := `{"type":"web_search_call","id":"search1","status":"completed"}`
	wire := "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":" + item + "}\n\nevent: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[" + item + "],\"usage\":{\"input_tokens\":12,\"output_tokens\":4}}}\n\n"
	req := &gateway.Request{Protocol: gateway.ProtocolResponses, Stream: true, Body: []byte(`{"tools":[{"type":"web_search_preview"}]}`)}
	s := (Provider{}).Decode(req, &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))})
	defer func() { _ = s.Close() }()
	var last *gateway.Usage
	for {
		ev, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if ev.Usage != nil {
			last = ev.Usage
		}
		if ev.Kind == gateway.EventDone {
			break
		}
	}
	if last == nil || last.ToolCalls["web_search_preview"] != 1 || last.Estimated || last.PromptTokens != 12 {
		t.Fatalf("usage=%+v", last)
	}
}

func TestSingleResponseCarriesToolsAndModalityUsage(t *testing.T) {
	for _, withTokens := range []bool{false, true} {
		usage := ""
		if withTokens {
			usage = `,"usage":{"input_tokens":120,"output_tokens":60,"input_tokens_details":{"audio_tokens":80,"image_tokens":20},"output_tokens_details":{"audio_tokens":40,"image_tokens":10}}`
		}
		body := `{"status":"completed","output":[{"type":"web_search_call","id":"s1"},{"type":"file_search_call","id":"f1"},{"type":"file_search_call","id":"f2"}]` + usage + `}`
		s := (Provider{}).Decode(&gateway.Request{Protocol: gateway.ProtocolResponses}, &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
		ev, err := s.Next()
		_ = s.Close()
		if err != nil || ev.Usage == nil || ev.Usage.ToolCalls["web_search"] != 1 || ev.Usage.ToolCalls["file_search"] != 2 || ev.Usage.Estimated == withTokens {
			t.Fatalf("usage=%+v err=%v", ev.Usage, err)
		}
		if withTokens && (ev.Usage.AudioInputTokens != 80 || ev.Usage.AudioOutputTokens != 40 || ev.Usage.ImageInputTokens != 20 || ev.Usage.ImageOutputTokens != 10) {
			t.Fatalf("missing modality=%+v", ev.Usage)
		}
	}
}
