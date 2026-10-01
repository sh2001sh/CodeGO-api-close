package vertex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestImagenChatUsesPredictAndPreservesLegacyParameters(t *testing.T) {
	req := &gateway.Request{Model: "picture", Protocol: gateway.ProtocolOpenAIChat,
		Body: []byte(`{"model":"picture","messages":[{"role":"system","content":"ignore this"},{"role":"user","content":[{"type":"text","text":"a cat"}]}],"n":1,"size":"1024x1024","extra_body":{"n":2,"parameters":{"aspectRatio":"16:9"}}}`)}
	out, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{UpstreamModel: "imagen-4.0-generate-001", Secret: "key"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	body, _ := io.ReadAll(out.Body)
	if out.URL.Path != "/v1/publishers/google/models/imagen-4.0-generate-001:predict" || gjson.GetBytes(body, "instances.0.prompt").Str != "a cat" ||
		gjson.GetBytes(body, "parameters.sampleCount").Int() != 2 || gjson.GetBytes(body, "parameters.aspectRatio").Str != "16:9" || gjson.GetBytes(body, "parameters.personGeneration").Str != "allow_adult" {
		t.Fatalf("wrong legacy Imagen conversion: %s %s", out.URL, body)
	}
}

func TestNativeActionsRetainBodyAndCredentialRouting(t *testing.T) {
	for _, action := range []string{"embedContent", "batchEmbedContents", "predict"} {
		model := "gemini-embedding-001"
		if action == "predict" {
			model = "imagen-4.0-generate-001"
		}
		req := &gateway.Request{Model: "alias", Protocol: gateway.ProtocolOpenAIChat, Body: []byte(`{"instances":[{"prompt":"hello"}]}`)}
		out, err := (Provider{}).BuildActionRequest(context.Background(), req,
			gateway.Target{UpstreamModel: model, Secret: encoded(t, Credentials{ProjectID: "p", AccessToken: "token", Regions: map[string]string{"alias": "us-east5"}})}, action)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(out.Body)
		_ = out.Body.Close()
		if out.Header.Get("Authorization") != "Bearer token" || !strings.Contains(out.URL.Path, "/projects/p/locations/us-east5/") || !strings.HasSuffix(out.URL.Path, ":"+action) || string(body) != string(req.Body) || req.Protocol != gateway.ProtocolOpenAIChat {
			t.Fatalf("native action lost metadata/body: %s %s", out.URL, body)
		}
	}
	if _, err := (Provider{}).BuildActionRequest(context.Background(), &gateway.Request{}, gateway.Target{}, "../arbitrary"); err == nil {
		t.Fatal("unrecognized action accepted")
	}
}

func TestImagenResponseUsageAndNativePayload(t *testing.T) {
	const prediction = `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="},{"raiFilteredReason":"blocked"}]}`
	for _, protocol := range []gateway.Protocol{gateway.ProtocolOpenAIChat, gateway.ProtocolGemini} {
		req := &gateway.Request{Model: "alias", Protocol: protocol}
		upstream, _ := http.NewRequest(http.MethodPost, "https://aiplatform.googleapis.com/v1/publishers/google/models/imagen:predict", nil)
		s := (Provider{}).Decode(req, &http.Response{Request: upstream, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(prediction))})
		event, err := s.Next()
		if err != nil || event.Kind != gateway.EventData || event.Usage == nil || event.Usage.PromptTokens != 258 || event.Usage.ImageOutputTokens != 258 || event.Usage.ImageCount != 1 || !event.Usage.Estimated {
			t.Fatalf("lost image usage: %+v %v", event, err)
		}
		if protocol == gateway.ProtocolGemini && string(event.Payload) != prediction {
			t.Fatal("native Imagen payload changed")
		}
		if protocol == gateway.ProtocolOpenAIChat && gjson.GetBytes(event.Payload, "data.0.b64_json").Str != "aW1hZ2U=" {
			t.Fatalf("image output was not normalized: %s", event.Payload)
		}
		if _, err := s.Next(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		_ = s.Close()
	}
	actual := &imageStream{req: &gateway.Request{Protocol: gateway.ProtocolGemini}, body: io.NopCloser(strings.NewReader(`{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":21,"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":21}]}}`))}
	event, err := actual.Next()
	_ = actual.Close()
	if err != nil || event.Usage.Estimated || event.Usage.PromptTokens != 7 || event.Usage.CompletionTokens != 21 || event.Usage.ImageOutputTokens != 21 || event.Usage.ImageCount != 1 {
		t.Fatalf("actual image metadata ignored: %+v %v", event, err)
	}
}

func TestImagenFailuresAreExplicit(t *testing.T) {
	for _, body := range []string{`{}`, `{"prompt":"cat","n":0}`, `{"prompt":"cat","n":1.5}`, `{"prompt":"cat","n":5}`, `{"prompt":"cat","size":"unknown"}`} {
		if _, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Model: "imagen", Protocol: gateway.ProtocolOpenAIChat, Body: []byte(body)}, gateway.Target{Secret: "key"}); err == nil {
			t.Fatalf("invalid Imagen request accepted: %s", body)
		}
	}
	if _, err := (Provider{}).BuildRequest(context.Background(), &gateway.Request{Model: "imagen", Protocol: gateway.ProtocolGemini, Stream: true, Body: []byte(`{}`)}, gateway.Target{Secret: "key"}); err == nil {
		t.Fatal("streaming Imagen accepted")
	}
	for _, body := range []string{`{}`, `{"predictions":[{"raiFilteredReason":"blocked"}]}`, `{"predictions":[{"bytesBase64Encoded":"not-base64"}]}`, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"busy"}}`} {
		s := &imageStream{req: &gateway.Request{}, body: io.NopCloser(strings.NewReader(body))}
		event, err := s.Next()
		_ = s.Close()
		if err == nil && event.Kind != gateway.EventError {
			t.Fatalf("invalid image response reported success: %+v", event)
		}
	}
}
