package auxiliary

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestVertexAuxiliaryNativeImagesPreserveFeatures(t *testing.T) {
	body := `{"instances":[{"prompt":"tree","referenceImages":[{"referenceId":1}]}],"parameters":{"sampleCount":1,"safetySetting":"block_low_and_above"},"custom":true}`
	response := `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="}],"custom":"native"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/p/locations/global/publishers/google/models/imagen-4.0-generate-001:predict" {
			t.Errorf("URL = %s", r.URL)
		}
		var want, actual any
		if err := json.Unmarshal([]byte(body), &want); err != nil {
			t.Error(err)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&actual); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(want, actual) {
			t.Errorf("native payload changed: %+v", actual)
		}
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()
	result, err := vertexTestExchange(t, &gateway.Request{Model: "image-alias", Protocol: gateway.ProtocolGemini},
		gateway.Target{BaseURL: server.URL, UpstreamModel: "imagen-4.0-generate-001", Secret: `{"project_id":"p","access_token":"direct-token"}`}, Input{Operation: GeminiImages, Body: []byte(body)})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Body) != response || result.Header.Get("X-Codego-Image-Count") != "1" || result.Usage == nil || !result.Usage.Estimated {
		t.Fatalf("response = %+v", result)
	}
}

func TestVertexAuxiliaryDefaultRegionalURL(t *testing.T) {
	for _, test := range []struct{ Credentials, Host, Path string }{
		{`{"project_id":"p","region":"us-east5","access_token":"token"}`, "us-east5-aiplatform.googleapis.com", "/v1/projects/p/locations/us-east5/publishers/google/models/gemini-embedding-001:embedContent"},
		{`{"project_id":"p","regions":{"alias":"europe-west4","default":"us-east5"},"access_token":"token"}`, "europe-west4-aiplatform.googleapis.com", "/v1/projects/p/locations/europe-west4/publishers/google/models/gemini-embedding-001:embedContent"},
		{`{"api_key":"key"}`, "aiplatform.googleapis.com", "/v1/publishers/google/models/gemini-embedding-001:embedContent"},
	} {
		upstream, err := vertexAdapter().Build(context.Background(), &gateway.Request{Model: "alias"}, gateway.Target{Secret: test.Credentials, UpstreamModel: "gemini-embedding-001"}, Input{Operation: GeminiEmbed, Body: []byte(`{"content":{"parts":[{"text":"test"}]}}`)})
		if err != nil {
			t.Fatal(err)
		}
		_ = upstream.Body.Close()
		if upstream.URL.Host != test.Host || upstream.URL.Path != test.Path {
			t.Errorf("URL = %s", upstream.URL)
		}
	}
}
