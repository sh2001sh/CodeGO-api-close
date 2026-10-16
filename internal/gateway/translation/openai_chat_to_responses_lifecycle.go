package translation

import (
	"fmt"
	"github.com/sh2001sh/new-api/dto"
)

func (s *ChatToResponsesStreamState) addReasoningDelta(delta string) []ResponsesBridgeEvent {
	events := s.closeMessage()
	if !s.reasoningOpen {
		s.reasoningOpen = true
		s.reasoningIndex = s.nextOutputIndex
		s.nextOutputIndex++
		s.reasoningID = fmt.Sprintf("rs_%s_%d", s.responseID, s.reasoningIndex)
		s.reasoningPart.Reset()
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
	s.reasoningPart.WriteString(delta)
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
	text := s.reasoningPart.String()
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
	return s.addMessageDelta(delta, "output_text")
}

func (s *ChatToResponsesStreamState) addMessageDelta(delta, kind string) []ResponsesBridgeEvent {
	events := s.closeReasoning()
	if s.messageOpen && s.messageKind != kind {
		events = append(events, s.closeMessage()...)
	}
	if !s.messageOpen {
		s.messageOpen = true
		s.messageIndex = s.nextOutputIndex
		s.nextOutputIndex++
		s.messageID = fmt.Sprintf("msg_%s_%d", s.responseID, s.messageIndex)
		s.messagePart.Reset()
		s.messageKind = kind
		item := &dto.ResponsesOutput{
			Type: "message", ID: s.messageID, Status: "in_progress", Role: "assistant", Content: []dto.ResponsesOutputContent{},
		}
		part := dto.ResponsesOutputContent{Type: kind, Text: "", Annotations: []interface{}{}}
		events = append(events,
			s.event("response.output_item.added", ResponsesBridgeEvent{
				OutputIndex: intPointer(s.messageIndex), Item: item,
			}),
			s.event("response.content_part.added", ResponsesBridgeEvent{
				ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Part: part,
			}),
		)
	}
	if kind == "refusal" {
		s.refusalText.WriteString(delta)
	} else {
		s.messageText.WriteString(delta)
	}
	s.messagePart.WriteString(delta)
	events = append(events, s.event("response."+kind+".delta", ResponsesBridgeEvent{
		ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Delta: delta,
	}))
	return events
}

func (s *ChatToResponsesStreamState) closeMessage() []ResponsesBridgeEvent {
	if !s.messageOpen {
		return nil
	}
	s.messageOpen = false
	text := s.messagePart.String()
	content := dto.ResponsesOutputContent{Type: s.messageKind, Text: text, Annotations: []interface{}{}}
	done := ResponsesBridgeEvent{ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Text: text}
	if s.messageKind == "refusal" {
		content.Text = ""
		content.Refusal = text
		done.Text = ""
		done.Refusal = text
	}
	item := dto.ResponsesOutput{
		Type: "message", ID: s.messageID, Status: "completed", Role: "assistant",
		Content: []dto.ResponsesOutputContent{content},
	}
	s.completedOutputs[s.messageIndex] = item
	return []ResponsesBridgeEvent{
		s.event("response."+s.messageKind+".done", done),
		s.event("response.content_part.done", ResponsesBridgeEvent{
			ItemID: s.messageID, OutputIndex: intPointer(s.messageIndex), ContentIndex: intPointer(0), Part: content,
		}),
		s.event("response.output_item.done", ResponsesBridgeEvent{
			OutputIndex: intPointer(s.messageIndex), Item: &item,
		}),
	}
}
