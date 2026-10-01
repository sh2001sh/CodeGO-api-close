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

func TestGeminiEmbeddingRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-embedding-001:batchEmbedContents" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Goog-Api-Key") != "test-key" || r.Header.Get("Authorization") != "" {
			t.Errorf("incorrect authentication headers")
		}
		var body struct {
			Requests []struct {
				Model      string `json:"model"`
				Dimensions int    `json:"outputDimensionality"`
				Content    struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if len(body.Requests) != 2 {
			t.Errorf("requests = %d", len(body.Requests))
			return
		}
		for i, request := range body.Requests {
			if request.Model != "models/gemini-embedding-001" || request.Dimensions != 2 || len(request.Content.Parts) != 1 {
				t.Errorf("request = %+v", request)
				return
			}
			if request.Content.Parts[0].Text != []string{"first", "second"}[i] {
				t.Errorf("wrong text")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embeddings":[{"values":[0.5,1]},{"values":[-1,0]}],"usageMetadata":{"promptTokenCount":8,"totalTokenCount":8}}`)
	}))
	defer server.Close()
	input := Input{Operation: Embeddings, Body: []byte(`{"model":"alias","input":["first","second"],"dimensions":2}`)}
	result, err := geminiTestExchange(t, server.URL, "gemini-embedding-001", input)
	if err != nil {
		t.Fatal(err)
	}
	var converted struct {
		Object, Model string
		Data          []struct {
			Index     int
			Embedding []float64
		}
		Usage struct {
			Prompt int `json:"prompt_tokens"`
		}
	}
	if err = json.Unmarshal(result.Body, &converted); err != nil {
		t.Fatal(err)
	}
	if converted.Object != "list" || converted.Model != "alias" || len(converted.Data) != 2 || converted.Data[1].Index != 1 || converted.Data[0].Embedding[0] != .5 {
		t.Fatalf("response = %s", result.Body)
	}
	if result.Usage == nil || result.Usage.PromptTokens != 8 || result.Usage.Estimated || converted.Usage.Prompt != 8 {
		t.Fatalf("usage = %+v / %s", result.Usage, result.Body)
	}
}

func TestGeminiNativeEmbeddingPreservation(t *testing.T) {
	for _, test := range []struct {
		Operation              Operation
		Body, Action, Response string
	}{
		{GeminiEmbed, `{"model":"models/old","content":{"parts":[{"text":"test"}]},"taskType":"RETRIEVAL_DOCUMENT","title":"title"}`, "embedContent", `{"embedding":{"values":[1,2]},"extra":"keep"}`},
		{GeminiBatchEmbed, `{"requests":[{"model":"models/old","content":{"parts":[{"text":"test"}]},"taskType":"RETRIEVAL_QUERY"}],"extra":"keep"}`, "batchEmbedContents", `{"embeddings":[{"values":[1,2]}],"extra":"keep"}`},
	} {
		t.Run(string(test.Operation), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models/mapped:"+test.Action {
					t.Errorf("path = %s", r.URL.Path)
				}
				data, _ := io.ReadAll(r.Body)
				if strings.Contains(string(data), "models/old") || !strings.Contains(string(data), "models/mapped") || !strings.Contains(string(data), "taskType") {
					t.Errorf("body = %s", data)
				}
				_, _ = io.WriteString(w, test.Response)
			}))
			defer server.Close()
			result, err := geminiTestExchange(t, server.URL+"/v1", "mapped", Input{Operation: test.Operation, Body: []byte(test.Body)})
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Body) != test.Response || result.Usage != nil {
				t.Fatalf("native response changed: %+v", result)
			}
		})
	}
}

func TestGeminiBuildRejectsUnsupportedAndInvalid(t *testing.T) {
	for _, test := range []struct {
		Operation   Operation
		Model, Body string
	}{
		{ImageEdits, "imagen-4", "multipart image"},
		{Speech, "gemini", `{}`},
		{Embeddings, "gemini-embedding-001", `{"input":[]}`},
		{Embeddings, "gemini-embedding-001", `{"input":[12]}`},
		{Embeddings, "gemini-embedding-001", `{"input":null}`},
		{Embeddings, "gemini-embedding-001", `{"input":" "}`},
		{Embeddings, "gemini-embedding-001", `{"input":"ok","dimensions":-1}`},
		{Embeddings, "embedding-001", `{"input":"ok","dimensions":2}`},
		{Embeddings, "gemini-embedding-001", `{"input":"ok","encoding_format":"other"}`},
		{GeminiEmbed, "model", `{"content":null}`},
		{GeminiBatchEmbed, "model", `{"requests":[null]}`},
		{GeminiBatchEmbed, "model", `{"requests":[{}]}`},
		{Images, "gemini", `{"prompt":"test"}`},
		{Images, "imagen", `{"prompt":"test","n":0}`},
		{Images, "imagen", `{"prompt":"test","response_format":"url"}`},
		{Images, "imagen", `{"prompt":"test","image":"ignored"}`},
		{Images, "imagen", `null`},
		{Images, "../escape", `{"prompt":"test"}`},
		{Images, "imagen:escape", `{"prompt":"test"}`},
		{GeminiImages, "gemini", `{"instances":[{"prompt":"test"}]}`},
	} {
		_, err := geminiAdapter().Build(context.Background(), &gateway.Request{Model: test.Model}, gateway.Target{BaseURL: "http://example.com"}, Input{Operation: test.Operation, Body: []byte(test.Body)})
		var upstream *gateway.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != 400 {
			t.Errorf("%s %s: error = %v", test.Operation, test.Body, err)
		}
	}
}

func TestGeminiBuildUsesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	upstream, err := geminiAdapter().Build(ctx, &gateway.Request{Model: "embedding-001"}, gateway.Target{BaseURL: "http://example.com"}, Input{Operation: Embeddings, Body: []byte(`{"input":"test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = http.DefaultClient.Do(upstream)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func geminiTestExchange(t *testing.T, base, model string, input Input) (Response, error) {
	t.Helper()
	req := &gateway.Request{Model: "alias", Body: []byte(`{"model":"billing-body-only"}`)}
	target := gateway.Target{BaseURL: base, UpstreamModel: model, Secret: "test-key"}
	adapter := geminiAdapter()
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
