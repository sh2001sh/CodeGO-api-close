package translation

import "github.com/sh2001sh/new-api/dto"

func (s *ChatToResponsesStreamState) addReasoningDelta(delta string) []ResponsesBridgeEvent {
	var events []ResponsesBridgeEvent
	if !s.reasoningOpen {
		s.reasoningOpen = true
		s.reasoningIndex = s.nextOutputIndex
		s.nextOutputIndex++
		s.reasoningID = "rs_" + s.responseID
		item := &dto.ResponsesOutput{Type: "reasoning", ID: s.reasoningID, Status: "in_progress"}
		events = append(events,
			s.event("response.output_item.added", ResponsesBridgeEvent{
				OutputIndex: intPointer(s.reasoningIndex), Item: item,
			}),
			s.event("response.reasoning_summary_part.added", ResponsesBridgeEvent{
				ItemID: s.reasoningID, OutputIndex: intPointer(s.reasoningIndex), SummaryIndex: intPointer(0),
				Part: dto.ResponsesReasoningSummaryPart{Type: "summary_text", Text: ""},
			}),
		)
	}
	s.reasoning.WriteString(delta)
	events = append(events, s.event("response.reasoning_summary_text.delta", ResponsesBridgeEvent{
		ItemID: s.reasoningID, OutputIndex: intPointer(s.reasoningIndex), SummaryIndex: intPointer(0), Delta: delta,
	}))
	return events
}

func (s *ChatToResponsesStreamState) closeReasoning() []ResponsesBridgeEvent {
	if !s.reasoningOpen {
		return nil
	}
	s.reasoningOpen = false
	text := s.reasoning.String()
	item := dto.ResponsesOutput{
		Type: "reasoning", ID: s.reasoningID, Status: "completed",
		Summary: []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: text}},
	}
	s.completedOutputs[s.reasoningIndex] = item
	return []ResponsesBridgeEvent{
		s.event("response.reasoning_summary_text.done", ResponsesBridgeEvent{
			ItemID: s.reasoningID, OutputIndex: intPointer(s.reasoningIndex), SummaryIndex: intPointer(0), Text: text,
		}),
		s.event("response.reasoning_summary_part.done", ResponsesBridgeEvent{
			ItemID: s.reasoningID, OutputIndex: intPointer(s.reasoningIndex), SummaryIndex: intPointer(0),
			Part: dto.ResponsesReasoningSummaryPart{Type: "summary_text", Text: text},
		}),
		s.event("response.output_item.done", ResponsesBridgeEvent{
			OutputIndex: intPointer(s.reasoningIndex), Item: &item,
		}),
	}
}

func (s *ChatToResponsesStreamState) addTextDelta(delta string) []ResponsesBridgeEvent {
	events := s.closeReasoning()
	if !s.messageOpen {
		s.messageOpen = true
		s.messageIndex = s.nextOutputIndex
		s.nextOutputIndex++
		s.messageID = "msg_" + s.responseID
		item := &dto.ResponsesOutput{
			Type: "message", ID: s.messageID, Status: "in_progress", Role: "assistant", Content: []dto.ResponsesOutputContent{},
		}
		part := dto.ResponsesOutputContent{Type: "output_text", Text: "", Annotations: []interface{}{}}
		events = append(events,
			s.event("response.output_item.added", ResponsesBridgeEvent{
				OutputIndex: intPointer(s.messageIndex), Item: item,
			}),
			s.event("response.content_part.added", ResponsesBridgeEvent{
				ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Part: part,
			}),
		)
	}
	s.messageText.WriteString(delta)
	events = append(events, s.event("response.output_text.delta", ResponsesBridgeEvent{
		ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Delta: delta,
	}))
	return events
}

func (s *ChatToResponsesStreamState) closeMessage() []ResponsesBridgeEvent {
	if !s.messageOpen {
		return nil
	}
	s.messageOpen = false
	text := s.messageText.String()
	content := dto.ResponsesOutputContent{Type: "output_text", Text: text, Annotations: []interface{}{}}
	item := dto.ResponsesOutput{
		Type: "message", ID: s.messageID, Status: "completed", Role: "assistant",
		Content: []dto.ResponsesOutputContent{content},
	}
	s.completedOutputs[s.messageIndex] = item
	return []ResponsesBridgeEvent{
		s.event("response.output_text.done", ResponsesBridgeEvent{
			ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Text: text,
		}),
		s.event("response.content_part.done", ResponsesBridgeEvent{
			ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Part: content,
		}),
		s.event("response.output_item.done", ResponsesBridgeEvent{
			OutputIndex: intPointer(s.messageIndex), Item: &item,
		}),
	}
}
