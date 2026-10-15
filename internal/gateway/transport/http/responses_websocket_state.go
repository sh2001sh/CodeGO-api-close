package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/internal/platform/runtime"
)

const (
	responsesWebsocketCreate = "response.create"
	responsesWebsocketAppend = "response.append"
)

type responsesWebsocketTurn struct {
	body             []byte
	referencedCached bool
	canMerge         bool
	prewarmPayloads  [][]byte
}

type responsesWebsocketProtocolError struct {
	status  int
	code    string
	message string
	param   string
}

func (e *responsesWebsocketProtocolError) Error() string { return e.message }

type responsesWebsocketState struct {
	lastRequest    []byte
	lastOutput     []byte
	lastResponseID string
	canMerge       bool
}

func (s *responsesWebsocketState) normalize(payload []byte) (*responsesWebsocketTurn, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, errors.New("invalid websocket request JSON")
	}

	eventType := rawJSONString(event["type"])
	if eventType != responsesWebsocketCreate && eventType != responsesWebsocketAppend {
		return nil, fmt.Errorf("unsupported websocket request type: %s", eventType)
	}

	previousID := rawJSONString(event["previous_response_id"])
	referencesCached := previousID != "" && previousID == s.lastResponseID
	shouldMerge := eventType == responsesWebsocketAppend || (referencesCached && s.canMerge)
	if eventType == responsesWebsocketAppend && (len(s.lastRequest) == 0 || !s.canMerge) {
		return nil, &responsesWebsocketProtocolError{
			status: 400, code: "previous_response_not_found", param: "previous_response_id",
			message: "Previous response is not available on this websocket; resend the full conversation input without previous_response_id",
		}
	}
	if (shouldMerge || referencesCached) && rawJSONString(event["model"]) == "" {
		var previous map[string]json.RawMessage
		if json.Unmarshal(s.lastRequest, &previous) == nil {
			event["model"] = bytes.Clone(previous["model"])
		}
	}

	request, err := normalizeResponsesWebsocketBody(event)
	if err != nil {
		return nil, err
	}
	canMerge := previousID == ""
	if shouldMerge {
		request, err = mergeResponsesWebsocketBody(s.lastRequest, s.lastOutput, request)
		if err != nil {
			return nil, err
		}
		canMerge = true
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal websocket request: %w", err)
	}
	turn := &responsesWebsocketTurn{
		body:             body,
		referencedCached: referencesCached || eventType == responsesWebsocketAppend,
		canMerge:         canMerge,
	}
	if rawJSONBool(event["generate"], true) == false {
		turn.prewarmPayloads, turn.body, err = buildResponsesWebsocketPrewarm(body)
	}
	return turn, err
}

func (s *responsesWebsocketState) complete(turn *responsesWebsocketTurn, responseID string, output []byte) {
	if turn == nil || responseID == "" {
		return
	}
	s.lastRequest = bytes.Clone(turn.body)
	s.lastOutput = bytes.Clone(output)
	if len(s.lastOutput) == 0 {
		s.lastOutput = []byte("[]")
	}
	s.lastResponseID = responseID
	s.canMerge = turn.canMerge
}

func (s *responsesWebsocketState) fail(turn *responsesWebsocketTurn) {
	if turn == nil || !turn.referencedCached {
		return
	}
	s.lastRequest = nil
	s.lastOutput = nil
	s.lastResponseID = ""
	s.canMerge = false
}

func normalizeResponsesWebsocketBody(event map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	request := cloneRawMessageMap(event)
	delete(request, "type")
	delete(request, "generate")
	delete(request, "background")
	request["stream"] = json.RawMessage("true")
	if _, ok := request["input"]; !ok {
		request["input"] = json.RawMessage("[]")
	}
	if strings.TrimSpace(rawJSONString(request["model"])) == "" {
		return nil, errors.New("missing model in response.create request")
	}
	return request, nil
}

func mergeResponsesWebsocketBody(previousBody, previousOutput []byte, next map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	var previous map[string]json.RawMessage
	if err := json.Unmarshal(previousBody, &previous); err != nil {
		return nil, errors.New("invalid cached websocket request")
	}
	merged := cloneRawMessageMap(previous)
	for key, value := range next {
		if key != "input" && key != "previous_response_id" {
			merged[key] = bytes.Clone(value)
		}
	}
	input, err := mergeResponsesWebsocketInput(previous["input"], previousOutput, next["input"])
	if err != nil {
		return nil, err
	}
	merged["input"] = input
	delete(merged, "previous_response_id")
	merged["stream"] = json.RawMessage("true")
	return merged, nil
}

func mergeResponsesWebsocketInput(parts ...[]byte) (json.RawMessage, error) {
	items := make([]json.RawMessage, 0)
	for _, part := range parts {
		parsed, err := responsesWebsocketInputItems(part)
		if err != nil {
			return nil, err
		}
		items = append(items, parsed...)
	}
	return json.Marshal(items)
}

func responsesWebsocketInputItems(raw []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, errors.New("websocket request requires a valid input array")
		}
		return items, nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil {
		return nil, errors.New("websocket continuation requires string or array input")
	}
	message, _ := json.Marshal(map[string]any{
		"type":    "message",
		"role":    "user",
		"content": []map[string]string{{"type": "input_text", "text": text}},
	})
	return []json.RawMessage{message}, nil
}

func buildResponsesWebsocketPrewarm(body []byte) ([][]byte, []byte, error) {
	responseID := "resp_prewarm_" + runtime.GetUUID()
	createdAt := time.Now().Unix()
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, nil, err
	}
	model := rawJSONString(request["model"])
	base := map[string]any{
		"id": responseID, "object": "response", "created_at": createdAt,
		"model": model, "background": false, "error": nil, "output": []any{},
	}
	createdResponse := cloneAnyMap(base)
	createdResponse["status"] = "in_progress"
	completedResponse := cloneAnyMap(base)
	completedResponse["status"] = "completed"
	completedResponse["usage"] = map[string]int{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
	created, _ := json.Marshal(map[string]any{"type": "response.created", "sequence_number": 0, "response": createdResponse})
	completed, _ := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": 1, "response": completedResponse})
	return [][]byte{created, completed}, body, nil
}

func cloneRawMessageMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		cloned[key] = bytes.Clone(value)
	}
	return cloned
}

func cloneAnyMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func rawJSONString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return strings.TrimSpace(value)
}

func rawJSONBool(raw json.RawMessage, fallback bool) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fallback
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return fallback
	}
	return value
}
