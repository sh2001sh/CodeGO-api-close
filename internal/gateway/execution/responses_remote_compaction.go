package execution

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/tidwall/sjson"
)

const remoteCompactionV1ViaV2ContextKey = "responses_compaction_v1_via_v2"

// buildRemoteCompactionV2Body preserves client request fields required by Codex.
func buildRemoteCompactionV2Body(c *gin.Context, originalModel string, mappedModel string, input json.RawMessage) (*bytes.Reader, int64, error) {
	storage, err := platformhttpx.GetBodyStorage(c)
	if err != nil {
		return nil, 0, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, 0, err
	}
	body, _, err = normalizeRemoteCompactionV2Body(body, originalModel, mappedModel)
	if err != nil {
		return nil, 0, err
	}
	if len(input) > 0 {
		body, err = sjson.SetRawBytes(body, "input", input)
		if err != nil {
			return nil, 0, fmt.Errorf("rewrite normalized response input: %w", err)
		}
	}
	return bytes.NewReader(body), int64(len(body)), nil
}

func normalizeRemoteCompactionInput(request *dto.OpenAIResponsesRequest) (bool, error) {
	if request == nil {
		return false, nil
	}
	return request.NormalizeCodexRemoteCompactionInput()
}

func normalizeRemoteCompactionV2Body(body []byte, originalModel, mappedModel string) ([]byte, bool, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("decode remote compaction v2 body: %w", err)
	}
	changed := false
	if strings.TrimSpace(originalModel) != strings.TrimSpace(mappedModel) && strings.TrimSpace(mappedModel) != "" {
		if payload["model"] != mappedModel {
			payload["model"] = mappedModel
			changed = true
		}
	}
	if stream, ok := payload["stream"].(bool); !ok || !stream {
		payload["stream"] = true
		changed = true
	}
	if input, ok := payload["input"].([]any); ok {
		triggerCount := 0
		normalized := make([]any, 0, len(input)+1)
		for _, item := range input {
			if object, ok := item.(map[string]any); ok && object["type"] == "compaction_trigger" {
				triggerCount++
				continue
			}
			normalized = append(normalized, item)
		}
		if triggerCount > 0 {
			normalized = append(normalized, map[string]any{"type": "compaction_trigger"})
			if triggerCount != 1 || len(input) == 0 {
				changed = true
			} else if last, ok := input[len(input)-1].(map[string]any); !ok || last["type"] != "compaction_trigger" {
				changed = true
			}
			payload["input"] = normalized
		}
	}
	if !changed {
		return body, false, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("encode remote compaction v2 body: %w", err)
	}
	return encoded, true, nil
}

func normalizeRemoteCompactionV1Body(body []byte) ([]byte, bool, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("decode remote compaction v1 body: %w", err)
	}
	changed := false
	if input, ok := payload["input"]; ok {
		request := &dto.OpenAIResponsesRequest{Input: input}
		inputChanged, err := normalizeRemoteCompactionInput(request)
		if err != nil {
			return nil, false, fmt.Errorf("normalize remote compaction v1 input: %w", err)
		}
		if inputChanged {
			payload["input"] = request.Input
			changed = true
		}
	}
	if _, ok := payload["stream"]; ok {
		delete(payload, "stream")
		changed = true
	}
	if !changed {
		return body, false, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("encode remote compaction v1 body: %w", err)
	}
	return encoded, true, nil
}

func buildRemoteCompactionV1ViaV2Body(body []byte) ([]byte, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode remote compaction v1 bridge body: %w", err)
	}
	input, ok := payload["input"].([]any)
	if !ok {
		return nil, errors.New("remote compaction v1 bridge requires an input array")
	}
	normalized := make([]any, 0, len(input)+1)
	for _, item := range input {
		if object, ok := item.(map[string]any); ok && object["type"] == "compaction_trigger" {
			continue
		}
		normalized = append(normalized, item)
	}
	payload["input"] = append(normalized, map[string]any{"type": "compaction_trigger"})
	payload["stream"] = true
	payload["store"] = false
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode remote compaction v1 bridge body: %w", err)
	}
	return encoded, nil
}

func addRemoteCompactionV2Header(header http.Header) {
	if header == nil {
		return
	}
	const feature = "remote_compaction_v2"
	for _, value := range header.Values("X-Codex-Beta-Features") {
		for _, item := range strings.Split(value, ",") {
			if strings.TrimSpace(item) == feature {
				return
			}
		}
	}
	current := strings.TrimSpace(header.Get("X-Codex-Beta-Features"))
	if current == "" {
		header.Set("X-Codex-Beta-Features", feature)
		return
	}
	header.Set("X-Codex-Beta-Features", current+", "+feature)
}

func remoteCompactionV2AsV1Response(response *http.Response) ([]byte, *dto.Usage, error) {
	if response == nil || response.Body == nil {
		return nil, nil, errors.New("remote compaction v2 bridge received an empty response")
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	var completed json.RawMessage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
			Error    json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, nil, fmt.Errorf("decode remote compaction v2 event: %w", err)
		}
		if event.Type == "error" || event.Type == "response.failed" || event.Type == "response.incomplete" {
			return nil, nil, fmt.Errorf("remote compaction v2 ended with %s: %s", event.Type, strings.TrimSpace(string(event.Error)))
		}
		if event.Type == "response.completed" && len(event.Response) > 0 {
			completed = append(completed[:0], event.Response...)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("read remote compaction v2 response: %w", err)
	}
	if len(completed) == 0 {
		return nil, nil, errors.New("remote compaction v2 response did not contain response.completed")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(completed, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode remote compaction v2 completed response: %w", err)
	}
	if !hasRemoteCompactionOutput(payload["output"]) {
		return nil, nil, errors.New("remote compaction v2 response did not contain a compaction output")
	}
	payload["object"] = json.RawMessage(`"response.compaction"`)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode remote compaction v1 response: %w", err)
	}
	var usageEnvelope struct {
		Usage *dto.Usage `json:"usage"`
	}
	if err := json.Unmarshal(completed, &usageEnvelope); err != nil {
		return nil, nil, fmt.Errorf("decode remote compaction usage: %w", err)
	}
	usage := usageEnvelope.Usage
	if usage == nil {
		usage = &dto.Usage{}
	}
	return encoded, usage, nil
}

func hasRemoteCompactionOutput(raw json.RawMessage) bool {
	var items []struct {
		Type string `json:"type"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, item := range items {
		if item.Type == "compaction" || item.Type == "compaction_summary" {
			return true
		}
	}
	return false
}

func hasRemoteCompactionTrigger(input json.RawMessage) bool {
	var items []map[string]any
	if len(input) == 0 || json.Unmarshal(input, &items) != nil {
		return false
	}
	for _, item := range items {
		if item["type"] == "compaction_trigger" {
			return true
		}
	}
	return false
}
