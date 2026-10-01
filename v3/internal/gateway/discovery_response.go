package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type discoveryModel struct {
	ID                     string   `json:"id"`
	Object                 string   `json:"object"`
	Created                int64    `json:"created"`
	OwnedBy                string   `json:"owned_by"`
	Description            string   `json:"description,omitempty"`
	Icon                   string   `json:"icon,omitempty"`
	Tags                   string   `json:"tags,omitempty"`
	SupportedEndpointTypes []string `json:"supported_endpoint_types,omitempty"`
	generationMethods      []string
}

type discoveryAnthropicModel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

type discoveryGeminiModel struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	Description                string   `json:"description,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
}

func discoveryProtocol(r *http.Request) Protocol {
	if strings.HasPrefix(r.URL.Path, "/v1beta/openai/") {
		return ProtocolOpenAIChat
	}
	if strings.HasPrefix(r.URL.Path, "/v1beta/models") || r.Header.Get("X-Goog-Api-Key") != "" || r.URL.Query().Get("key") != "" {
		return ProtocolGemini
	}
	if r.Header.Get("X-Api-Key") != "" && r.Header.Get("Anthropic-Version") != "" {
		return ProtocolAnthropic
	}
	return ProtocolOpenAIChat
}

func discoveryItem(protocol Protocol, model discoveryModel) any {
	switch protocol {
	case ProtocolAnthropic:
		return discoveryAnthropicModel{ID: model.ID, Type: "model", DisplayName: model.ID, CreatedAt: time.Unix(model.Created, 0).UTC().Format(time.RFC3339)}
	case ProtocolGemini:
		return discoveryGeminiModel{Name: model.ID, DisplayName: model.ID, Description: model.Description, SupportedGenerationMethods: model.generationMethods}
	default:
		return model
	}
}

func discoveryList(protocol Protocol, models []discoveryModel) any {
	switch protocol {
	case ProtocolAnthropic:
		data := make([]discoveryAnthropicModel, 0, len(models))
		for _, model := range models {
			data = append(data, discoveryItem(protocol, model).(discoveryAnthropicModel))
		}
		first, last := "", ""
		if len(models) > 0 {
			first, last = models[0].ID, models[len(models)-1].ID
		}
		return struct {
			Data    []discoveryAnthropicModel `json:"data"`
			FirstID string                    `json:"first_id"`
			LastID  string                    `json:"last_id"`
			HasMore bool                      `json:"has_more"`
		}{data, first, last, false}
	case ProtocolGemini:
		data := make([]discoveryGeminiModel, 0, len(models))
		for _, model := range models {
			data = append(data, discoveryItem(protocol, model).(discoveryGeminiModel))
		}
		return struct {
			Models        []discoveryGeminiModel `json:"models"`
			NextPageToken *string                `json:"nextPageToken"`
		}{Models: data}
	default:
		return struct {
			Success bool             `json:"success"`
			Object  string           `json:"object"`
			Data    []discoveryModel `json:"data"`
		}{true, "list", models}
	}
}

func discoveryJSON(w http.ResponseWriter, value any) {
	// All values are local DTOs with no fallible custom marshalers.
	body, err := json.Marshal(value)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "api_error", "model_encoding_failed", "model metadata could not be encoded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
