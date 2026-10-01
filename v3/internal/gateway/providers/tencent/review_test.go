package tencent

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestChannelConfigurationFailuresCanRetryAnotherCredential(t *testing.T) {
	for _, target := range []gateway.Target{{Secret: "invalid"}, {Secret: "123|test-id|test-secret", BaseURL: "ftp://invalid"}} {
		_, err := (Provider{}).BuildRequest(context.Background(), fixtureRequest(`{"messages":[{"role":"user","content":"hello"}]}`, false), target)
		var failure *gateway.UpstreamError
		if !errors.As(err, &failure) || failure.Status != http.StatusBadGateway || failure.Type != "upstream_error" {
			t.Fatalf("channel failure incorrectly classified as caller input: %v", err)
		}
	}
}

func TestNativeErrorDoesNotReflectSignedCredentials(t *testing.T) {
	s := fixtureStream(false, `{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"test-secret test-id Authorization=TC3-HMAC-SHA256"}}}`, false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err != nil || ev.Kind != gateway.EventError || ev.Err == nil || strings.Contains(ev.Err.Error(), "test-secret") || strings.Contains(ev.Err.Error(), "test-id") || strings.Contains(ev.Err.Error(), "Authorization") || ev.Err.Code != "AuthFailure.SignatureFailure" {
		t.Fatalf("unsafe error: %+v %v", ev, err)
	}
}

func TestSingleResponseWithoutFinishReasonCannotComplete(t *testing.T) {
	s := fixtureStream(false, `{"Response":{"Choices":[{"Message":{"Role":"assistant","Content":"partial"}}],"Usage":{"PromptTokens":2,"CompletionTokens":1}}}`, false)
	defer func() { _ = s.Close() }()
	ev, err := s.Next()
	if err == nil && ev.Kind != gateway.EventError {
		t.Fatalf("unterminated single response accepted: %+v", ev)
	}
}
