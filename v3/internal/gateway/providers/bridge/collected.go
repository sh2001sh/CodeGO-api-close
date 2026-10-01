package bridge

import (
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Some subscription upstreams stream even when the caller requests JSON. Keep
// that response private until its terminal usage arrives, then emit one body.
type collectedResponse struct {
	started                  bool
	text, reasoning, refusal strings.Builder
	tools                    toolCollector
}

func (c *collectedResponse) add(delta chatDelta) error {
	if delta.Content != "" || delta.ReasoningContent != "" || delta.Refusal != "" || len(delta.ToolCalls) > 0 {
		c.started = true
	}
	c.text.WriteString(delta.Content)
	c.reasoning.WriteString(delta.ReasoningContent)
	c.refusal.WriteString(delta.Refusal)
	return c.tools.add(delta.ToolCalls)
}
func (s *stream) finishCollected() error {
	tools, err := s.collected.tools.finish()
	if err != nil {
		return err
	}
	delta := chatDelta{Content: s.collected.text.String(), ReasoningContent: s.collected.reasoning.String(), Refusal: s.collected.refusal.String(), ToolCalls: tools}
	payload, err := singleResponse(s.protocol, s.responses.id, s.responses.model, delta, s.finishReason, s.usage)
	if err != nil {
		return err
	}
	s.queue = append(s.queue, gateway.Event{Kind: gateway.EventData, Payload: payload, Usage: s.usage}, gateway.Event{Kind: gateway.EventDone})
	return nil
}
