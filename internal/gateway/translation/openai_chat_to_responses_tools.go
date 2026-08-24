package translation

import (
	"errors"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/dto"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
)

func (s *ChatToResponsesStreamState) addToolDelta(call dto.ToolCallResponse) ([]ResponsesBridgeEvent, error) {
	events := s.closeReasoning()
	events = append(events, s.closeMessage()...)
	index := 0
	if call.Index != nil {
		index = *call.Index
	}
	tool := s.tools[index]
	if tool == nil {
		tool = &chatResponsesToolState{index: index, outputIndex: -1}
		s.tools[index] = tool
	}
	if call.ID != "" {
		tool.callID = call.ID
	}
	if call.Function.Name != "" {
		tool.name = call.Function.Name
	}
	argumentDelta := call.Function.Arguments
	if argumentDelta != "" {
		tool.arguments.WriteString(argumentDelta)
	}
	if !tool.opened && tool.name != "" {
		events = append(events, s.openTool(tool)...)
		if tool.arguments.Len() > 0 {
			events = append(events, s.toolArgumentDelta(tool, tool.arguments.String())...)
		}
		return events, nil
	}
	if tool.opened && argumentDelta != "" {
		events = append(events, s.toolArgumentDelta(tool, argumentDelta)...)
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) openTool(tool *chatResponsesToolState) []ResponsesBridgeEvent {
	tool.opened = true
	tool.outputIndex = s.nextOutputIndex
	s.nextOutputIndex++
	tool.itemID = toolItemID(s.responseID, tool.index)
	if tool.callID == "" {
		tool.callID = tool.itemID
	}
	tool.itemType, tool.originalName, tool.namespace = s.classifyTool(tool.name)
	item := tool.responsesItem("in_progress")
	return []ResponsesBridgeEvent{s.event("response.output_item.added", ResponsesBridgeEvent{
		OutputIndex: intPointer(tool.outputIndex), Item: &item,
	})}
}

func (s *ChatToResponsesStreamState) toolArgumentDelta(tool *chatResponsesToolState, delta string) []ResponsesBridgeEvent {
	base := ResponsesBridgeEvent{
		ItemID: tool.itemID, OutputIndex: intPointer(tool.outputIndex), Delta: delta,
	}
	switch tool.itemType {
	case "custom_tool_call":
		base.Delta = unwrapCustomToolArguments(delta)
		return []ResponsesBridgeEvent{s.event("response.custom_tool_call_input.delta", base)}
	case "tool_search_call":
		return nil
	default:
		return []ResponsesBridgeEvent{s.event("response.function_call_arguments.delta", base)}
	}
}

func (s *ChatToResponsesStreamState) closeTools() ([]ResponsesBridgeEvent, error) {
	var events []ResponsesBridgeEvent
	for index := 0; index < len(s.tools); index++ {
		tool := s.tools[index]
		if tool == nil {
			continue
		}
		if tool.name == "" {
			return nil, errors.New("chat completions tool call completed without a name")
		}
		if !tool.opened {
			events = append(events, s.openTool(tool)...)
			if tool.arguments.Len() > 0 {
				events = append(events, s.toolArgumentDelta(tool, tool.arguments.String())...)
			}
		}
		item := tool.responsesItem("completed")
		events = append(events, s.closeToolEvents(tool, item)...)
		s.completedOutputs[tool.outputIndex] = item
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) closeToolEvents(tool *chatResponsesToolState, item dto.ResponsesOutput) []ResponsesBridgeEvent {
	arguments := tool.arguments.String()
	var events []ResponsesBridgeEvent
	switch tool.itemType {
	case "custom_tool_call":
		events = append(events, s.event("response.custom_tool_call_input.done", ResponsesBridgeEvent{
			ItemID: tool.itemID, OutputIndex: intPointer(tool.outputIndex), Input: unwrapCustomToolArguments(arguments),
		}))
	case "tool_search_call":
	default:
		events = append(events, s.event("response.function_call_arguments.done", ResponsesBridgeEvent{
			ItemID: tool.itemID, OutputIndex: intPointer(tool.outputIndex), Arguments: arguments,
		}))
	}
	return append(events, s.event("response.output_item.done", ResponsesBridgeEvent{
		OutputIndex: intPointer(tool.outputIndex), Item: &item,
	}))
}

func (s *ChatToResponsesStreamState) classifyTool(name string) (itemType, originalName, namespace string) {
	itemType = "function_call"
	originalName = name
	if s.meta == nil {
		return
	}
	if mapped, ok := s.meta.NamespaceTools[name]; ok {
		originalName = mapped.Name
		namespace = mapped.Namespace
	}
	if _, ok := s.meta.CustomToolNames[name]; ok {
		itemType = "custom_tool_call"
	}
	if _, ok := s.meta.ToolSearchNames[name]; ok {
		itemType = "tool_search_call"
	}
	return
}

func (t *chatResponsesToolState) responsesItem(status string) dto.ResponsesOutput {
	item := dto.ResponsesOutput{
		Type: t.itemType, ID: t.itemID, Status: status, CallId: t.callID,
		Name: t.originalName, Namespace: t.namespace,
	}
	arguments := t.arguments.String()
	switch t.itemType {
	case "custom_tool_call":
		item.Input = unwrapCustomToolArguments(arguments)
	default:
		item.Arguments = argumentsRaw(arguments)
	}
	return item
}

func argumentsRaw(arguments string) []byte {
	raw, _ := platformencoding.Marshal(arguments)
	return raw
}

func toolItemID(responseID string, index int) string {
	return strings.ReplaceAll(responseID, "resp_", "fc_") + "_" + strconv.Itoa(index)
}
