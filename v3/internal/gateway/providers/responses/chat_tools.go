package responses

import (
	"encoding/json"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func marshalChat(value any) []byte { body, _ := json.Marshal(value); return body }

func (s *chatStream) callItem(item gjson.Result, outputIndex int, complete bool) *gateway.UpstreamError {
	call := s.tools[outputIndex]
	if call == nil {
		if item.Get("call_id").Str == "" || item.Get("name").Str == "" {
			return chatOutputError()
		}
		call = &chatCall{index: len(s.tools), id: item.Get("call_id").Str, name: item.Get("name").Str}
		s.tools[outputIndex] = call
		if id := item.Get("id").Str; id != "" {
			s.toolIDs[id] = outputIndex
		}
		tool := chatTool{Index: call.index, ID: call.id, Type: "function", Function: map[string]any{"name": call.name, "arguments": ""}}
		s.queue = append(s.queue, s.chunk(map[string]any{"tool_calls": []chatTool{tool}}, nil, 0))
	} else if item.Get("call_id").Str != call.id || item.Get("name").Str != call.name {
		return chatOutputError()
	}
	arguments := item.Get("arguments").Str
	if complete || arguments != "" {
		if len(arguments) < call.argumentBytes {
			return chatOutputError()
		}
		if len(arguments) > call.argumentBytes {
			delta := arguments[call.argumentBytes:]
			call.argumentBytes = len(arguments)
			s.emitArguments(call, delta)
		}
	}
	return nil
}

func (s *chatStream) callDelta(root gjson.Result, outputIndex int) *gateway.UpstreamError {
	if !root.Get("output_index").Exists() {
		var ok bool
		outputIndex, ok = s.toolIDs[root.Get("item_id").Str]
		if !ok {
			return chatOutputError()
		}
	}
	call := s.tools[outputIndex]
	if call == nil {
		return chatOutputError()
	}
	delta := root.Get("delta").Str
	call.argumentBytes += len(delta)
	if delta != "" {
		s.emitArguments(call, delta)
	}
	return nil
}

func (s *chatStream) emitArguments(call *chatCall, delta string) {
	tool := chatTool{Index: call.index, Function: map[string]any{"arguments": delta}}
	s.queue = append(s.queue, s.chunk(map[string]any{"tool_calls": []chatTool{tool}}, nil, len(delta)))
}
