package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/dto"
	"github.com/sh2001sh/new-api/types"
)

const StreamUsageRecoveryTimeout = 2 * time.Minute

// Bound abandoned upstream work independently of the number of live streams.
var streamUsageRecoverySlots = make(chan struct{}, 32)

// StreamUsageRecovery keeps an accepted HTTP stream alive briefly after its
// downstream disappears. Before acceptance, cancellation still stops the dial
// or request immediately. Close must be called with the response body.
type StreamUsageRecovery struct {
	mu                                          sync.Mutex
	ctx                                         context.Context
	cancel                                      context.CancelFunc
	stopParent                                  func() bool
	timer                                       *time.Timer
	grace                                       time.Duration
	accepted, attempted, draining, closed, slot bool
	end                                         string
	usage                                       *dto.Usage
	terminal                                    bool
	failure                                     *types.OpenAIError
	text                                        strings.Builder
	textTruncated                               bool
}

func NewStreamUsageRecovery(parent context.Context, maxDuration, grace time.Duration) *StreamUsageRecovery {
	if maxDuration <= 0 {
		maxDuration = 30 * time.Minute
	}
	if grace <= 0 {
		grace = StreamUsageRecoveryTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), maxDuration)
	r := &StreamUsageRecovery{ctx: ctx, cancel: cancel, grace: grace}
	r.stopParent = context.AfterFunc(parent, func() { r.BeginDrain() })
	return r
}

func (r *StreamUsageRecovery) Context() context.Context { return r.ctx }

func (r *StreamUsageRecovery) Accept() {
	r.mu.Lock()
	r.accepted = true
	r.mu.Unlock()
}

func (r *StreamUsageRecovery) BeginDrain() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	if r.draining {
		return true
	}
	r.attempted = r.accepted
	if !r.accepted || r.ctx.Err() != nil {
		r.cancel()
		return false
	}
	select {
	case streamUsageRecoverySlots <- struct{}{}:
		r.slot, r.draining = true, true
		r.timer = time.AfterFunc(r.grace, func() {
			r.mu.Lock()
			if !r.closed {
				r.end = "recovery_timeout"
				r.cancel()
			}
			r.mu.Unlock()
		})
		return true
	default:
		r.end = "recovery_capacity_exceeded"
		r.cancel()
		return false
	}
}

func (r *StreamUsageRecovery) IsDraining() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.draining
}

func (r *StreamUsageRecovery) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.stopParent != nil {
		r.stopParent()
	}
	if r.timer != nil {
		r.timer.Stop()
	}
	if r.slot {
		<-streamUsageRecoverySlots
		r.slot = false
	}
	r.cancel()
}

// Observe runs in the scanner before downstream delivery. Never retain model
// content in logs; bounded text is used only if final provider usage is missing.
func (r *StreamUsageRecovery) Observe(data string) {
	if r == nil {
		return
	}
	var event struct {
		Type     string                                    `json:"type"`
		Delta    string                                    `json:"delta"`
		Error    any                                       `json:"error"`
		Usage    *dto.Usage                                `json:"usage"`
		Response *dto.OpenAIResponsesResponse              `json:"response"`
		Choices  []dto.ChatCompletionsStreamResponseChoice `json:"choices"`
	}
	if json.Unmarshal([]byte(data), &event) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if failure := (dto.ResponsesStreamResponse{Type: event.Type, Response: event.Response, Error: event.Error}).TerminalError(); failure != nil {
		r.failure = failure
		r.terminal = true
		r.end = "upstream_failed"
		return
	}
	terminalResponse := false
	switch event.Type {
	case "response.completed", "response.incomplete", "response.cancelled", "response.canceled", "response.failed":
		terminalResponse = true
		r.terminal = true
	}
	// Ignore usage placeholders on response.created and intermediate chunks.
	// A usage chunk or a finished choice is authoritative even if [DONE] is lost.
	finishedChoices := true
	for _, choice := range event.Choices {
		finishedChoices = finishedChoices && choice.FinishReason != nil && *choice.FinishReason != ""
	}
	if event.Type == "" && finishedChoices && event.Usage != nil && (event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 || event.Usage.InputTokens > 0 || event.Usage.OutputTokens > 0) {
		r.usage = event.Usage
		r.terminal = true
	}
	if terminalResponse && event.Response != nil && event.Response.Usage != nil && (event.Response.Usage.TotalTokens > 0 || event.Response.Usage.InputTokens > 0 || event.Response.Usage.OutputTokens > 0) {
		u := event.Response.Usage
		copy := *u
		r.usage = &copy
	}
	switch event.Type {
	case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.refusal.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		r.appendText(event.Delta)
	}
	for _, choice := range event.Choices {
		r.appendText(choice.Delta.GetContentString())
		r.appendText(choice.Delta.GetReasoningContent())
		for _, tool := range choice.Delta.ToolCalls {
			r.appendText(tool.Function.Name)
			r.appendText(tool.Function.Arguments)
		}
	}
}

func (r *StreamUsageRecovery) appendText(text string) {
	const maxTextBytes = 1 << 20
	if r.text.Len()+len(text) > maxTextBytes {
		r.textTruncated = true
		return
	}
	r.text.WriteString(text)
}

func (r *StreamUsageRecovery) Done() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.terminal = true
	r.mu.Unlock()
}

func (r *StreamUsageRecovery) Terminal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.terminal
}

func (r *StreamUsageRecovery) Failure() *types.OpenAIError {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure == nil {
		return nil
	}
	copy := *r.failure
	return &copy
}

func (r *StreamUsageRecovery) Snapshot() (*dto.Usage, string, map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var usage *dto.Usage
	if r.usage != nil {
		copy := *r.usage
		if copy.InputTokens > 0 {
			copy.PromptTokens = copy.InputTokens
		}
		if copy.OutputTokens > 0 {
			copy.CompletionTokens = copy.OutputTokens
		}
		if copy.InputTokensDetails != nil {
			copy.PromptTokensDetails = *copy.InputTokensDetails
		}
		copy.PromptTokensDetails.CachedCreationTokens = copy.PromptTokensDetails.GetCachedCreationTokens()
		if copy.OutputTokensDetails != nil {
			copy.CompletionTokenDetails = *copy.OutputTokensDetails
		}
		usage = &copy
	}
	end := r.end
	if end == "" {
		switch {
		case usage != nil:
			end = "final_usage_received"
		case r.ctx.Err() == context.DeadlineExceeded:
			end = "max_duration"
		case r.terminal:
			end = "terminal_without_usage"
		default:
			end = "closed_without_final_usage"
		}
	}
	return usage, r.text.String(), map[string]interface{}{
		"attempted": r.attempted, "final_usage_received": usage != nil,
		"terminal_event_received": r.terminal, "end_reason": end,
		"observed_text_truncated": r.textTruncated,
	}
}
