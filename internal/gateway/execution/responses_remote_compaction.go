package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/tidwall/sjson"
)

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
