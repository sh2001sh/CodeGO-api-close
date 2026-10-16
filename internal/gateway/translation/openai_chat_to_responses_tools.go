package translation

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

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
	if index < 0 {
		return nil, errors.New("chat completions tool call index must be non-negative")
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
	if !tool.opened && tool.name != "" && tool.callID != "" {
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
		input, ok := customToolInputPrefix(tool.arguments.String())
		if !ok || !strings.HasPrefix(input, tool.emittedInput) {
			return nil
		}
		base.Delta = strings.TrimPrefix(input, tool.emittedInput)
		tool.emittedInput = input
		if base.Delta == "" {
			return nil
		}
		return []ResponsesBridgeEvent{s.event("response.custom_tool_call_input.delta", base)}
	case "tool_search_call":
		return nil
	default:
		return []ResponsesBridgeEvent{s.event("response.function_call_arguments.delta", base)}
	}
}

func (s *ChatToResponsesStreamState) closeTools() ([]ResponsesBridgeEvent, error) {
	var events []ResponsesBridgeEvent
	indexes := make([]int, 0, len(s.tools))
	for index := range s.tools {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
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
		input := unwrapCustomToolArguments(arguments)
		if strings.HasPrefix(input, tool.emittedInput) && input != tool.emittedInput {
			events = append(events, s.event("response.custom_tool_call_input.delta", ResponsesBridgeEvent{
				ItemID: tool.itemID, OutputIndex: intPointer(tool.outputIndex), Delta: strings.TrimPrefix(input, tool.emittedInput),
			}))
		}
		events = append(events, s.event("response.custom_tool_call_input.done", ResponsesBridgeEvent{
			ItemID: tool.itemID, OutputIndex: intPointer(tool.outputIndex), Input: input,
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

// Decode the complete prefix of a JSON-wrapped custom-tool string, keeping
// partial escapes for the next chunk instead of leaking JSON syntax to clients.
func customToolInputPrefix(arguments string) (string, bool) {
	value := strings.TrimSpace(arguments)
	if !strings.HasPrefix(value, "{") {
		return "", false
	}
	value = strings.TrimSpace(value[1:])
	if !strings.HasPrefix(value, `"input"`) {
		return "", false
	}
	value = strings.TrimSpace(value[len(`"input"`):])
	if !strings.HasPrefix(value, ":") {
		return "", false
	}
	value = strings.TrimSpace(value[1:])
	if !strings.HasPrefix(value, `"`) {
		return "", false
	}
	var decoded strings.Builder
	for index := 1; index < len(value); {
		if value[index] == '"' {
			return decoded.String(), true
		}
		if value[index] != '\\' {
			r, size := utf8.DecodeRuneInString(value[index:])
			if r == utf8.RuneError && size == 1 {
				return decoded.String(), true
			}
			decoded.WriteRune(r)
			index += size
			continue
		}
		if index+1 >= len(value) {
			break
		}
		if value[index+1] == 'u' {
			if index+6 > len(value) {
				break
			}
			n, err := strconv.ParseUint(value[index+2:index+6], 16, 16)
			if err != nil {
				return "", false
			}
			r := rune(n)
			index += 6
			if r >= 0xd800 && r <= 0xdbff {
				if index+6 > len(value) {
					break
				}
				if value[index:index+2] != `\u` {
					return "", false
				}
				low, err := strconv.ParseUint(value[index+2:index+6], 16, 16)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return "", false
				}
				r = utf16.DecodeRune(r, rune(low))
				index += 6
			} else if r >= 0xdc00 && r <= 0xdfff {
				return "", false
			}
			decoded.WriteRune(r)
			continue
		}
		escaped := value[index : index+2]
		if escaped == `\/` {
			decoded.WriteByte('/')
		} else {
			var unescaped string
			if err := platformencoding.Unmarshal([]byte(`"`+escaped+`"`), &unescaped); err != nil {
				return "", false
			}
			decoded.WriteString(unescaped)
		}
		index += 2
	}
	return decoded.String(), true
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
