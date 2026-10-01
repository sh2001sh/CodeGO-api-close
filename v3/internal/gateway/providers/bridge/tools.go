package bridge

import (
	"fmt"
	"sort"
	"strings"
)

type collectedTool struct {
	call      chatToolCall
	arguments strings.Builder
}
type toolCollector struct{ calls map[int]*collectedTool }

func (c *toolCollector) add(calls []chatToolCall) error {
	if c.calls == nil {
		c.calls = make(map[int]*collectedTool)
	}
	for _, call := range calls {
		if call.Index < 0 {
			return fmt.Errorf("bridge: negative tool-call index")
		}
		state := c.calls[call.Index]
		if state == nil {
			state = &collectedTool{}
			c.calls[call.Index] = state
		}
		old := &state.call
		old.Index = call.Index
		if call.ID != "" {
			old.ID = call.ID
		}
		old.Function.Name += call.Function.Name
		state.arguments.WriteString(call.Function.Arguments)
	}
	return nil
}
func (c *toolCollector) finish() ([]chatToolCall, error) {
	indices := make([]int, 0, len(c.calls))
	for index := range c.calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	out := make([]chatToolCall, 0, len(indices))
	for _, index := range indices {
		state := c.calls[index]
		call := state.call
		call.Function.Arguments = state.arguments.String()
		if call.Function.Name == "" {
			return nil, fmt.Errorf("bridge: missing completed tool-call name")
		}
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", index)
		}
		args, err := toolArguments(call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		call.Function.Arguments = string(args)
		out = append(out, call)
	}
	return out, nil
}
