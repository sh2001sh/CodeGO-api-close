package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type responsePart struct {
	Type        string   `json:"type"`
	Text        string   `json:"text,omitempty"`
	Refusal     string   `json:"refusal,omitempty"`
	Annotations []string `json:"annotations,omitempty"`
}

func (p responsePart) MarshalJSON() ([]byte, error) {
	if p.Type == "output_text" {
		annotations := p.Annotations
		if annotations == nil {
			annotations = []string{}
		}
		return json.Marshal(struct {
			Type        string   `json:"type"`
			Text        string   `json:"text"`
			Annotations []string `json:"annotations"`
		}{p.Type, p.Text, annotations})
	}
	if p.Type == "refusal" {
		return json.Marshal(struct {
			Type    string `json:"type"`
			Refusal string `json:"refusal"`
		}{p.Type, p.Refusal})
	}
	return json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{p.Type, p.Text})
}

type responseItem struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Status    string         `json:"status,omitempty"`
	Role      string         `json:"role,omitempty"`
	Content   []responsePart `json:"content,omitempty"`
	Summary   []responsePart `json:"summary,omitempty"`
	CallID    string         `json:"call_id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments *string        `json:"arguments,omitempty"`
}
type responseUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	InputDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}
type responseWire struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	Status            string             `json:"status"`
	Model             string             `json:"model"`
	Output            []responseItem     `json:"output"`
	Usage             *responseUsage     `json:"usage,omitempty"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details,omitempty"`
}
type incompleteDetails struct {
	Reason string `json:"reason"`
}
type responseEvent struct {
	Type         string        `json:"type"`
	Sequence     int           `json:"sequence_number"`
	Response     *responseWire `json:"response,omitempty"`
	OutputIndex  *int          `json:"output_index,omitempty"`
	ContentIndex *int          `json:"content_index,omitempty"`
	SummaryIndex *int          `json:"summary_index,omitempty"`
	ItemID       string        `json:"item_id,omitempty"`
	Item         *responseItem `json:"item,omitempty"`
	Part         *responsePart `json:"part,omitempty"`
	Delta        *string       `json:"delta,omitempty"`
	Text         *string       `json:"text,omitempty"`
	Arguments    *string       `json:"arguments,omitempty"`
	Refusal      *string       `json:"refusal,omitempty"`
}

func responseUsageOf(u *gateway.Usage) *responseUsage {
	if u == nil {
		return nil
	}
	r := &responseUsage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, TotalTokens: u.PromptTokens + u.CompletionTokens}
	r.InputDetails.CachedTokens = u.CachedTokens
	return r
}
func pointer[T any](v T) *T { return &v }
func toolArguments(s string) (json.RawMessage, error) {
	if s == "" {
		s = "{}"
	}
	if !json.Valid([]byte(s)) || !gjson.Parse(s).IsObject() {
		return nil, fmt.Errorf("bridge: invalid completed tool-call arguments")
	}
	return json.RawMessage(s), nil
}

func singleResponse(protocol gateway.Protocol, id, model string, delta chatDelta, reason string, usage *gateway.Usage) ([]byte, error) {
	switch protocol {
	case gateway.ProtocolResponses:
		r := responseWire{ID: id, Object: "response", Status: "completed", Model: model, Output: []responseItem{}, Usage: responseUsageOf(usage)}
		if reason == "length" || reason == "content_filter" {
			r.Status = "incomplete"
			r.IncompleteDetails = &incompleteDetails{Reason: responseIncompleteReason(reason)}
		}
		if delta.ReasoningContent != "" {
			r.Output = append(r.Output, responseItem{ID: id + "_reasoning", Type: "reasoning", Status: "completed", Summary: []responsePart{{Type: "summary_text", Text: delta.ReasoningContent}}})
		}
		if delta.Content != "" || delta.Refusal != "" {
			item := responseItem{ID: id + "_message", Type: "message", Status: "completed", Role: "assistant", Content: []responsePart{}}
			if delta.Content != "" {
				item.Content = append(item.Content, responsePart{Type: "output_text", Text: delta.Content})
			}
			if delta.Refusal != "" {
				item.Content = append(item.Content, responsePart{Type: "refusal", Refusal: delta.Refusal})
			}
			r.Output = append(r.Output, item)
		}
		for index, tool := range delta.ToolCalls {
			if _, err := toolArguments(tool.Function.Arguments); err != nil {
				return nil, err
			}
			arguments, err := toolArguments(tool.Function.Arguments)
			if err != nil {
				return nil, err
			}
			r.Output = append(r.Output, responseItem{ID: fmt.Sprintf("%s_tool_%d", id, index), Type: "function_call", Status: "completed", CallID: tool.ID, Name: tool.Function.Name, Arguments: pointer(string(arguments))})
		}
		return json.Marshal(r)
	case gateway.ProtocolAnthropic:
		return anthropicSingle(id, model, delta, reason, usage)
	case gateway.ProtocolGemini:
		return geminiSingle(model, delta, reason, usage)
	default:
		return nil, fmt.Errorf("bridge: unsupported output protocol")
	}
}
func responseIncompleteReason(reason string) string {
	if reason == "length" {
		return "max_output_tokens"
	}
	return "content_filter"
}
