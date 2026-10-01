package bridge

import (
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Regression (protocol review): tool-only Chat streams were translated into
// native lifecycle frames with zero byte estimates and therefore billed free.
func TestToolOnlyStreamsAccountArgumentsOnceBeforeFinalization(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		events := []gateway.Event{chunk(`{"tool_calls":[{"index":0,"id":"call","function":{"name":"lookup","arguments":"{\"word\":"}}]}`, nil), chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"hello\"}"}}]}`, nil), {Kind: gateway.EventDone}}
		out := decode(t, request(protocol, true), events, nil)
		consumed := 0
		beforeDone := 0
		for _, event := range out {
			consumed += event.TextBytes
			if event.Kind == gateway.EventDone {
				beforeDone = consumed
			}
		}
		if consumed != len(`{"word":"hello"}`) || beforeDone != consumed {
			t.Fatalf("protocol %d estimated %d bytes want %d", protocol, consumed, len(`{"word":"hello"}`))
		}
	}
}

func TestPartialToolArgumentsRemainAccountedWhenUpstreamFails(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		failure := &gateway.UpstreamError{Status: 502, Type: "upstream_error", Code: "failed"}
		out := decode(t, request(protocol, true), []gateway.Event{chunk(`{"content":"ok"}`, nil), chunk(`{"tool_calls":[{"index":0,"id":"call","function":{"name":"lookup","arguments":"{\"word\":"}}]}`, nil), chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"part"}}]}`, nil), {Kind: gateway.EventError, Err: failure}}, nil)
		consumed := 0
		for _, event := range out {
			consumed += event.TextBytes
			if event.Name == "response.completed" || event.Name == "message_stop" || event.Kind == gateway.EventDone {
				t.Fatal("failed stream completed")
			}
		}
		if consumed != len("ok")+len(`{"word":"part`) {
			t.Fatalf("protocol %d discarded partial tool bytes: %d", protocol, consumed)
		}
	}
}

func TestUpstreamByteAccountingIsRetainedAcrossNativeLifecycleFrames(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		event := chunk(`{"content":"x"}`, nil)
		event.TextBytes = 99
		out := decode(t, request(protocol, true), []gateway.Event{event, {Kind: gateway.EventDone}}, nil)
		sum := 0
		for _, event := range out {
			sum += event.TextBytes
		}
		if sum != 99 {
			t.Fatalf("protocol %d upstream byte accounting changed to %d", protocol, sum)
		}
	}
}
