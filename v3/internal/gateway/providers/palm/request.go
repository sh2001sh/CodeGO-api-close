package palm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

type nativeMessage struct {
	Author  string `json:"author"`
	Content string `json:"content"`
}

type nativePrompt struct {
	Context  string          `json:"context,omitempty"`
	Messages []nativeMessage `json:"messages"`
}

type nativeRequest struct {
	Prompt         nativePrompt `json:"prompt"`
	Temperature    *float64     `json:"temperature,omitempty"`
	CandidateCount *int         `json:"candidateCount,omitempty"`
	TopP           *float64     `json:"topP,omitempty"`
	TopK           *int         `json:"topK,omitempty"`
}

func convertRequest(body []byte) ([]byte, error) {
	var input map[string]json.RawMessage
	if err := json.Unmarshal(body, &input); err != nil || input == nil {
		return nil, errors.New("PaLM requires a JSON object")
	}
	for key, value := range input {
		switch key {
		case "model", "messages", "temperature", "top_p", "top_k", "n", "stream", "stream_options", "user":
		default:
			if string(value) != "null" {
				return nil, fmt.Errorf("PaLM does not support %s", key)
			}
		}
	}
	var out nativeRequest
	var err error
	if out.Temperature, err = probability(input["temperature"], "temperature"); err != nil {
		return nil, err
	}
	if out.TopP, err = probability(input["top_p"], "top_p"); err != nil {
		return nil, err
	}
	if out.TopK, err = positiveInteger(input["top_k"], "top_k", 0); err != nil {
		return nil, err
	}
	if out.CandidateCount, err = positiveInteger(input["n"], "n", 8); err != nil {
		return nil, err
	}
	if err := validateStreamOptions(input["stream_options"]); err != nil {
		return nil, err
	}
	if err := populatePrompt(&out, input["messages"]); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func validateStreamOptions(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(raw, &options); err != nil || options == nil {
		return errors.New("PaLM requires an object for stream_options")
	}
	for key, value := range options {
		var flag bool
		if key != "include_usage" || json.Unmarshal(value, &flag) != nil || string(value) == "null" {
			return errors.New("PaLM supports only boolean stream_options.include_usage")
		}
	}
	return nil
}

// populatePrompt validates the OpenAI-style messages array and converts it
// into PaLM's context + conversation prompt shape.
func populatePrompt(out *nativeRequest, raw json.RawMessage) error {
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil || len(messages) == 0 {
		return errors.New("PaLM requires nonempty messages")
	}
	var context []string
	for _, message := range messages {
		for key, value := range message {
			if key != "role" && key != "content" && string(value) != "null" {
				return fmt.Errorf("PaLM does not support message.%s", key)
			}
		}
		var role string
		if err := json.Unmarshal(message["role"], &role); err != nil {
			return errors.New("PaLM requires a message role")
		}
		text, err := messageText(message["content"])
		if err != nil {
			return err
		}
		switch role {
		case "system", "developer":
			if len(out.Prompt.Messages) != 0 {
				return errors.New("PaLM only supports instructions before conversation messages")
			}
			context = append(context, text)
		case "user", "assistant":
			out.Prompt.Messages = append(out.Prompt.Messages, nativeMessage{Author: role, Content: text})
		default:
			return fmt.Errorf("PaLM does not support role %q", role)
		}
	}
	if len(out.Prompt.Messages) == 0 {
		return errors.New("PaLM requires conversation messages")
	}
	out.Prompt.Context = strings.Join(context, "\n")
	return nil
}

func messageText(raw json.RawMessage) (string, error) {
	var text string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", err
		}
	} else {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &parts); err != nil || len(parts) == 0 {
			return "", errors.New("PaLM supports nonempty text content only")
		}
		for _, part := range parts {
			if part.Type != "text" {
				return "", errors.New("PaLM supports text content only")
			}
			text += part.Text
		}
	}
	if text == "" {
		return "", errors.New("PaLM requires nonempty message content")
	}
	return text, nil
}

func probability(raw json.RawMessage, field string) (*float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return nil, fmt.Errorf("PaLM %s must be a number between 0 and 1", field)
	}
	return &value, nil
}

func positiveInteger(raw json.RawMessage, field string, limit int) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil || value < 1 || (limit > 0 && value > limit) {
		return nil, fmt.Errorf("PaLM %s must be a positive integer within its supported range", field)
	}
	return &value, nil
}
