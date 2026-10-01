package auxiliary

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestGeminiImageConversionAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/imagen-4:predict" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body struct {
			Instances []struct {
				Prompt string `json:"prompt"`
			} `json:"instances"`
			Parameters struct {
				Count   int    `json:"sampleCount"`
				Aspect  string `json:"aspectRatio"`
				Quality string `json:"imageSize"`
				Person  string `json:"personGeneration"`
			} `json:"parameters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Instances) != 1 || body.Instances[0].Prompt != "a tree" || body.Parameters.Count != 3 || body.Parameters.Aspect != "16:9" || body.Parameters.Quality != "2K" || body.Parameters.Person != "allow_adult" {
			t.Errorf("payload = %+v", body)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", "native-body-hash")
		_, _ = io.WriteString(w, `{"predictions":[{"bytesBase64Encoded":"aW1hZ2Ux"},{"raiFilteredReason":"blocked"},{"bytesBase64Encoded":"aW1hZ2Uy"}]}`)
	}))
	defer server.Close()
	result, err := geminiTestExchange(t, server.URL, "imagen-4", Input{Operation: Images, Body: []byte(`{"prompt":"a tree","size":"1792x1024","quality":"high","n":3,"response_format":"b64_json"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var converted struct {
		Created int64
		Data    []struct {
			B64 string `json:"b64_json"`
		}
	}
	if err = json.Unmarshal(result.Body, &converted); err != nil {
		t.Fatal(err)
	}
	if converted.Created == 0 || len(converted.Data) != 2 || converted.Data[1].B64 != "aW1hZ2Uy" {
		t.Fatalf("body = %s", result.Body)
	}
	if result.Usage == nil || result.Usage.PromptTokens != 516 || result.Usage.ImageOutputTokens != 516 || !result.Usage.Estimated {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.Header.Get("Content-Type") != "application/json" || result.Header.Get("Content-Length") != "" || result.Header.Get("ETag") != "" {
		t.Fatalf("headers = %v", result.Header)
	}
	if result.Header.Get("X-Codego-Image-Count") != "2" {
		t.Fatalf("image count includes a filtered prediction: %v", result.Header)
	}
}

func TestGeminiNativeImagesKeepPayloadAndActualUsage(t *testing.T) {
	body := `{"instances":[{"prompt":"native"}],"parameters":{"sampleCount":1,"safetySetting":"block_low_and_above"},"extra":true}`
	response := `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U="}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":21,"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":21}]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), `"safetySetting":"block_low_and_above"`) || !strings.Contains(string(data), `"extra":true`) {
			t.Errorf("body = %s", data)
		}
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()
	result, err := geminiTestExchange(t, server.URL, "imagen-4", Input{Operation: GeminiImages, Body: []byte(body)})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Body) != response || result.Usage == nil || result.Usage.PromptTokens != 3 || result.Usage.CompletionTokens != 21 || result.Usage.ImageOutputTokens != 21 || result.Usage.Estimated {
		t.Fatalf("response = %+v", result)
	}
	if result.Header.Get("X-Codego-Image-Count") != "1" {
		t.Fatalf("native image count missing: %v", result.Header)
	}
}

func TestGeminiEmbeddingBase64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"embeddings":[{"values":[1,-0.5]}]}`)
	}))
	defer server.Close()
	result, err := geminiTestExchange(t, server.URL, "gemini-embedding-001", Input{Operation: Embeddings, Body: []byte(`{"input":"text","encoding_format":"base64"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct{ Data []struct{ Embedding string } }
	if err = json.Unmarshal(result.Body, &decoded); err != nil {
		t.Fatal(err)
	}
	value, err := base64.StdEncoding.DecodeString(decoded.Data[0].Embedding)
	if err != nil || string(value) != string([]byte{0, 0, 128, 63, 0, 0, 0, 191}) {
		t.Fatalf("embedding = %x, error = %v", value, err)
	}
}

func TestGeminiDecodeFailureAndBoundaries(t *testing.T) {
	for _, test := range []struct {
		Operation Operation
		Body      string
		Status    int
	}{
		{GeminiEmbed, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"rate limit"}}`, 429},
		{GeminiEmbed, `{"error":{"code":0,"message":"bad"}}`, 502},
		{GeminiEmbed, `{"embedding":{"values":[]}}`, 0},
		{GeminiBatchEmbed, `{"embeddings":[]}`, 0},
		{GeminiBatchEmbed, `{"embeddings":[{"values":[]}]}`, 0},
		{Embeddings, `{"embeddings":[{"values":[1]},{"values":[2]}]}`, 0},
		{GeminiImages, `{"predictions":[{"raiFilteredReason":"blocked"}]}`, 400},
		{GeminiImages, `{"predictions":[{"bytesBase64Encoded":"invalid!"}]}`, 0},
		{GeminiImages, `{"predictions":[{"bytesBase64Encoded":""}]}`, 0},
		{GeminiEmbed, `null`, 0},
		{GeminiEmbed, `{`, 0},
	} {
		resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.Body))}
		_, err := geminiAdapter().Decode(context.Background(), &gateway.Request{}, gateway.Target{}, Input{Operation: test.Operation, Body: []byte(`{"input":"one"}`)}, resp)
		if err == nil {
			t.Errorf("%s accepted %s", test.Operation, test.Body)
			continue
		}
		if test.Status > 0 {
			var upstream *gateway.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != test.Status {
				t.Errorf("error = %v", err)
			}
		}
	}
	resp := &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":403}}`))}
	result, err := geminiAdapter().Decode(context.Background(), &gateway.Request{}, gateway.Target{}, Input{Operation: GeminiEmbed}, resp)
	if err != nil || string(result.Body) != `{"error":{"code":403}}` {
		t.Fatalf("HTTP error response = %+v, %v", result, err)
	}
}
