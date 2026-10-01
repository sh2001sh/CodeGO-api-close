package bridge

import (
	"net/http"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type nativeTransportProvider struct{ fakeProvider }

func (*nativeTransportProvider) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

func TestTransportSelectionKeepsNativeAndProxyFallback(t *testing.T) {
	native := &nativeTransportProvider{}
	chat := &fakeProvider{}
	fallback := &http.Transport{}
	provider := Provider{Chat: chat, Native: map[gateway.Protocol]gateway.Provider{gateway.ProtocolResponses: native}}
	if got := provider.UpstreamTransport(&gateway.Request{Protocol: gateway.ProtocolResponses}, fallback); got != native {
		t.Fatalf("native transport hidden by wrapper: %T", got)
	}
	if got := provider.UpstreamTransport(&gateway.Request{Protocol: gateway.ProtocolOpenAIChat}, fallback); got != fallback {
		t.Fatalf("configured HTTP transport replaced: %T", got)
	}
}
