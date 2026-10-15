package translation

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/dto"
)

type ResponsesBridgeEvent struct {
	Type           string                       `json:"type"`
	SequenceNumber int                          `json:"sequence_number"`
	Response       *dto.OpenAIResponsesResponse `json:"response,omitempty"`
	OutputIndex    *int                         `json:"output_index,omitempty"`
	ContentIndex   *int                         `json:"content_index,omitempty"`
	SummaryIndex   *int                         `json:"summary_index,omitempty"`
	ItemID         string                       `json:"item_id,omitempty"`
	Item           *dto.ResponsesOutput         `json:"item,omitempty"`
	Part           any                          `json:"part,omitempty"`
	Delta          string                       `json:"delta,omitempty"`
	Text           string                       `json:"text,omitempty"`
	Arguments      string                       `json:"arguments,omitempty"`
	Input          string                       `json:"input,omitempty"`
}

type chatResponsesToolState struct {
	index        int
	outputIndex  int
	itemID       string
	callID       string
	name         string
	arguments    strings.Builder
	opened       bool
	itemType     string
	namespace    string
	originalName string
}

type ChatToResponsesStreamState struct {
	request          *dto.OpenAIResponsesRequest
	meta             *ResponsesChatBridgeMeta
	responseID       string
	createdAt        int
	model            string
	sequence         int
	started          bool
	finishReason     string
	nextOutputIndex  int
	reasoningOpen    bool
	reasoningIndex   int
	reasoningID      string
	reasoning        strings.Builder
	messageOpen      bool
	messageIndex     int
	messageID        string
	messageText      strings.Builder
	tools            map[int]*chatResponsesToolState
	completedOutputs map[int]dto.ResponsesOutput
	usage            *dto.Usage
}

func NewChatToResponsesStreamState(req *dto.OpenAIResponsesRequest, meta *ResponsesChatBridgeMeta) *ChatToResponsesStreamState {
	return &ChatToResponsesStreamState{
		request: req, meta: meta, createdAt: int(time.Now().Unix()),
		tools: make(map[int]*chatResponsesToolState), completedOutputs: make(map[int]dto.ResponsesOutput),
	}
}

func (s *ChatToResponsesStreamState) ConvertChunk(chunk *dto.ChatCompletionsStreamResponse) ([]ResponsesBridgeEvent, error) {
	if chunk == nil {
		return nil, nil
	}
	s.observeChunkMetadata(chunk)
	events := s.ensureStarted()
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			return nil, errors.New("multiple chat completion choices cannot be represented by one responses stream")
		}
		var err error
		events, err = s.convertChoice(events, choice)
		if err != nil {
			return nil, err
		}
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) Finalize() ([]ResponsesBridgeEvent, *dto.Usage, error) {
	events := s.ensureStarted()
	events = append(events, s.closeReasoning()...)
	events = append(events, s.closeMessage()...)
	toolEvents, err := s.closeTools()
	if err != nil {
		return nil, nil, err
	}
	events = append(events, toolEvents...)
	completed := s.responseSnapshot(s.completionStatus(), true)
	events = append(events, s.event("response."+s.completionStatus(), ResponsesBridgeEvent{Response: completed}))
	return events, s.usage, nil
}

func (s *ChatToResponsesStreamState) UsageText() string {
	var text strings.Builder
	text.WriteString(s.reasoning.String())
	text.WriteString(s.messageText.String())
	for index := 0; index < len(s.tools); index++ {
		if tool := s.tools[index]; tool != nil {
			text.WriteString(tool.name)
			text.WriteString(tool.arguments.String())
		}
	}
	return text.String()
}

func (s *ChatToResponsesStreamState) Usage() *dto.Usage {
	return s.usage
}

func (s *ChatToResponsesStreamState) SetUsage(usage *dto.Usage) {
	s.usage = usage
}

func (s *ChatToResponsesStreamState) convertChoice(events []ResponsesBridgeEvent, choice dto.ChatCompletionsStreamResponseChoice) ([]ResponsesBridgeEvent, error) {
	delta := choice.Delta
	if reasoning := delta.GetReasoningContent(); reasoning != "" {
		events = append(events, s.addReasoningDelta(reasoning)...)
	}
	if content := delta.GetContentString(); content != "" {
		events = append(events, s.addTextDelta(content)...)
	}
	for _, toolCall := range delta.ToolCalls {
		toolEvents, err := s.addToolDelta(toolCall)
		if err != nil {
			return nil, err
		}
		events = append(events, toolEvents...)
	}
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		s.finishReason = *choice.FinishReason
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) observeChunkMetadata(chunk *dto.ChatCompletionsStreamResponse) {
	if s.responseID == "" && chunk.Id != "" {
		s.responseID = responsesIDFromChat(chunk.Id)
	}
	if s.responseID == "" {
		s.responseID = responsesIDFromChat("")
	}
	if chunk.Created > 0 {
		s.createdAt = int(chunk.Created)
	}
	if chunk.Model != "" {
		s.model = chunk.Model
	}
	if chunk.Usage != nil {
		s.usage = chatUsageToResponsesUsage(*chunk.Usage)
	}
}

func (s *ChatToResponsesStreamState) ensureStarted() []ResponsesBridgeEvent {
	if s.started {
		return nil
	}
	if s.responseID == "" {
		s.responseID = responsesIDFromChat("")
	}
	if s.model == "" && s.request != nil {
		s.model = s.request.Model
	}
	s.started = true
	created := s.responseSnapshot("in_progress", false)
	inProgress := s.responseSnapshot("in_progress", false)
	return []ResponsesBridgeEvent{
		s.event("response.created", ResponsesBridgeEvent{Response: created}),
		s.event("response.in_progress", ResponsesBridgeEvent{Response: inProgress}),
	}
}

func (s *ChatToResponsesStreamState) responseSnapshot(status string, includeOutput bool) *dto.OpenAIResponsesResponse {
	statusRaw, _ := jsonString(status)
	response := &dto.OpenAIResponsesResponse{
		ID: s.responseID, Object: "response", CreatedAt: s.createdAt,
		Status: statusRaw, Model: s.model, Usage: s.usage,
	}
	applyResponsesRequestEcho(response, s.request)
	if includeOutput {
		indexes := make([]int, 0, len(s.completedOutputs))
		for index := range s.completedOutputs {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			response.Output = append(response.Output, s.completedOutputs[index])
		}
	}
	if status == "incomplete" {
		response.IncompleteDetails = &dto.IncompleteDetails{Reasoning: "max_output_tokens"}
	}
	return response
}

func (s *ChatToResponsesStreamState) completionStatus() string {
	if s.finishReason == "length" {
		return "incomplete"
	}
	return "completed"
}

func (s *ChatToResponsesStreamState) event(eventType string, event ResponsesBridgeEvent) ResponsesBridgeEvent {
	event.Type = eventType
	event.SequenceNumber = s.sequence
	s.sequence++
	return event
}

func jsonString(value string) ([]byte, error) {
	return []byte(fmt.Sprintf("%q", value)), nil
}

func intPointer(value int) *int { return &value }
