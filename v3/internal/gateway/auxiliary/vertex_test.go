package auxiliary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestVertexAuxiliaryImagesBearerRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/v1/projects/test-project/locations/us-east5/publishers/google/models/imagen-4.0-generate-001:predict" || r.URL.Query().Get("custom") != "keep" {
			t.Errorf("URL = %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer direct-token" || r.Header.Get("X-Goog-User-Project") != "test-project" || r.Header.Get("X-Goog-Api-Key") != "" || r.URL.Query().Get("key") != "" {
			t.Errorf("incorrect Vertex authentication")
		}
		var body struct {
			Instances []struct {
				Prompt string `json:"prompt"`
			} `json:"instances"`
			Parameters struct {
				N      int    `json:"sampleCount"`
				Aspect string `json:"aspectRatio"`
				Size   string `json:"imageSize"`
			} `json:"parameters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Instances) != 1 || body.Instances[0].Prompt != "tree" || body.Parameters.N != 2 || body.Parameters.Aspect != "3:2" || body.Parameters.Size != "2K" {
			t.Errorf("body = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="},{"raiFilteredReason":"blocked"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":32,"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":32}]}}`)
	}))
	defer server.Close()
	req := &gateway.Request{Model: "image-alias", Protocol: gateway.ProtocolOpenAIChat, Body: []byte(`{"model":"billing-only"}`)}
	target := gateway.Target{BaseURL: server.URL + "/custom/v1?custom=keep", UpstreamModel: "imagen-4.0-generate-001", Secret: `{"project_id":"test-project","region":"europe-west4","regions":{"image-alias":"us-east5"},"access_token":"direct-token"}`}
	input := Input{Operation: Images, Body: []byte(`{"model":"image-alias","prompt":"tree","n":2,"size":"1536x1024","quality":"high"}`)}
	result, err := vertexTestExchange(t, req, target, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Body), `"b64_json":"aW1hZ2U="`) || result.Header.Get("X-Codego-Image-Count") != "1" {
		t.Fatalf("response = %+v", result)
	}
	if result.Usage == nil || result.Usage.PromptTokens != 4 || result.Usage.ImageOutputTokens != 32 || result.Usage.Estimated {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if req.Protocol != gateway.ProtocolOpenAIChat || req.Model != "image-alias" || string(req.Body) != `{"model":"billing-only"}` {
		t.Fatalf("request mutated: %+v", req)
	}
}

func TestVertexAuxiliaryNativeEmbeddingsAPIKey(t *testing.T) {
	for _, test := range []struct {
		Operation              Operation
		Action, Body, Response string
	}{
		{GeminiEmbed, "embedContent", `{"model":"models/old","content":{"parts":[{"text":"one"}]},"taskType":"RETRIEVAL_QUERY","title":"keep"}`, `{"embedding":{"values":[1,2]},"extra":"native"}`},
		{GeminiBatchEmbed, "batchEmbedContents", `{"requests":[{"model":"models/old","content":{"parts":[{"text":"one"}]},"taskType":"RETRIEVAL_DOCUMENT"}],"extra":true}`, `{"embeddings":[{"values":[1,2]}],"extra":"native"}`},
	} {
		t.Run(string(test.Operation), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/projects/project/locations/europe-west4/publishers/google/models/gemini-embedding-001:"+test.Action || r.URL.Query().Get("key") != "vertex-api-key" {
					t.Errorf("URL = %s", r.URL)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Goog-Api-Key") != "" {
					t.Errorf("unexpected authentication headers")
				}
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "models/old") || !strings.Contains(string(body), "models/gemini-embedding-001") || !strings.Contains(string(body), "taskType") {
					t.Errorf("body = %s", body)
				}
				_, _ = io.WriteString(w, test.Response)
			}))
			defer server.Close()
			req := &gateway.Request{Model: "vector-alias", Protocol: gateway.ProtocolGemini}
			target := gateway.Target{BaseURL: server.URL, UpstreamModel: "gemini-embedding-001", Secret: `{"project_id":"project","regions":{"default":"europe-west4"},"api_key":"vertex-api-key"}`}
			result, err := vertexTestExchange(t, req, target, Input{Operation: test.Operation, Body: []byte(test.Body)})
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Body) != test.Response {
				t.Fatalf("native response changed: %s", result.Body)
			}
		})
	}
}

func TestVertexAuxiliaryOpenAIEmbeddingConversion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/publishers/google/models/gemini-embedding-001:batchEmbedContents" || r.URL.Query().Get("key") != "plain-key" {
			t.Errorf("URL = %s", r.URL)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, ok := body["input"]; ok || !strings.Contains(string(body["requests"]), `"outputDimensionality":2`) {
			t.Errorf("body = %v", body)
		}
		_, _ = io.WriteString(w, `{"embeddings":[{"values":[1,2]},{"values":[3,4]}],"usageMetadata":{"promptTokenCount":7}}`)
	}))
	defer server.Close()
	result, err := vertexTestExchange(t, &gateway.Request{Model: "vector-alias"}, gateway.Target{BaseURL: server.URL, UpstreamModel: "gemini-embedding-001", Secret: "plain-key"}, Input{Operation: Embeddings, Body: []byte(`{"input":["first","second"],"dimensions":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	var converted struct {
		Object, Model string
		Data          []struct {
			Index     int
			Embedding []float64
		}
	}
	if err := json.Unmarshal(result.Body, &converted); err != nil {
		t.Fatal(err)
	}
	if converted.Object != "list" || converted.Model != "vector-alias" || len(converted.Data) != 2 || converted.Data[1].Embedding[0] != 3 || result.Usage == nil || result.Usage.PromptTokens != 7 {
		t.Fatalf("response = %+v", result)
	}
}

func TestVertexAuxiliaryFailures(t *testing.T) {
	for _, test := range []struct {
		Operation           Operation
		Model, Secret, Body string
	}{
		{Images, "imagen", "", `{"prompt":"image"}`},
		{Images, "imagen", `{`, `{"prompt":"image"}`},
		{Images, "imagen", `{"project_id":"p","client_email":"account"}`, `{"prompt":"image"}`},
		{Images, "imagen", `{"api_key":"key","access_token":"token"}`, `{"prompt":"image"}`},
		{Images, "imagen", `{"access_token":"token","region":"../invalid"}`, `{"prompt":"image"}`},
		{Images, "imagen", "key", `{}`},
		{Images, "claude", "key", `{"prompt":"image"}`},
		{ImageEdits, "imagen", "key", "multipart"},
		{Embeddings, "embedding-001", "key", `{"input":[42]}`},
	} {
		_, err := vertexAdapter().Build(context.Background(), &gateway.Request{Model: test.Model}, gateway.Target{Secret: test.Secret}, Input{Operation: test.Operation, Body: []byte(test.Body)})
		if err == nil {
			t.Errorf("accepted invalid %s request", test.Operation)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := vertexAdapter().Build(ctx, &gateway.Request{Model: "imagen"}, gateway.Target{Secret: "key"}, Input{Operation: Images, Body: []byte(`{"prompt":"image"}`)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v", err)
	}
	if _, err := vertexAdapter().Build(context.Background(), nil, gateway.Target{}, Input{}); err == nil {
		t.Fatal("nil request accepted")
	}
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"limited"}}`))}
	_, err = vertexAdapter().Decode(context.Background(), &gateway.Request{}, gateway.Target{}, Input{Operation: Images}, response)
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 429 {
		t.Fatalf("decode error = %v", err)
	}
}

func vertexTestExchange(t *testing.T, req *gateway.Request, target gateway.Target, input Input) (Response, error) {
	t.Helper()
	adapter := vertexAdapter()
	upstream, err := adapter.Build(context.Background(), req, target, input)
	if err != nil {
		return Response{}, err
	}
	resp, err := http.DefaultClient.Do(upstream)
	if err != nil {
		return Response{}, err
	}
	return adapter.Decode(context.Background(), req, target, input, resp)
}
