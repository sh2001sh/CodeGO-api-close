package auxiliary

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func vectorTestResponse(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Length": []string{"999"}, "Content-Encoding": []string{"gzip"}}}
}

func TestVectorNativeResponseConversion(t *testing.T) {
	cases := []struct {
		provider      string
		op            Operation
		body          string
		input, output int64
		checks        map[string]string
	}{
		{"ollama", Embeddings, `{"embeddings":[[0.1,0.2],[0.3,0.4]],"prompt_eval_count":14}`, 14, 0, map[string]string{"object": "list", "model": "client", "data.1.object": "embedding", "data.1.index": "1", "data.1.embedding.0": "0.3", "usage.total_tokens": "14"}},
		{"cohere", Rerank, `{"results":[{"index":0,"relevance_score":0.9,"document":{"text":"one"}}],"meta":{"billed_units":{"input_tokens":14,"output_tokens":2}}}`, 14, 2, map[string]string{"results.0.document.text": "one", "usage.total_tokens": "16"}},
		{"ali", Rerank, `{"output":{"results":[{"index":1,"relevance_score":0.4}]},"usage":{"total_tokens":17}}`, 17, 0, map[string]string{"results.0.index": "1", "usage.prompt_tokens": "17"}},
		{"jina", Rerank, `{"results":[{"index":0,"relevance_score":0.2}],"usage":{"total_tokens":8}}`, 8, 0, map[string]string{"usage.total_tokens": "8"}},
		{"cloudflare", Embeddings, `{"success":true,"result":{"data":[{"embedding":[1,2]}],"usage":{"prompt_tokens":19,"total_tokens":19}}}`, 19, 0, map[string]string{"object": "list", "data.0.index": "0", "data.0.embedding.1": "2", "model": "client"}},
		{"baidu", Embeddings, `{"data":[{"object":"embedding","index":0,"embedding":[1,2]}],"usage":{"prompt_tokens":21,"total_tokens":21}}`, 21, 0, map[string]string{"model": "client", "data.0.embedding.0": "1"}},
		{"mokaai", Embeddings, `{"data":[{"embedding":[1,2]}],"usage":{"prompt_tokens":5,"total_tokens":5}}`, 5, 0, map[string]string{"model": "client", "object": "list"}},
		{"volcengine", Embeddings, `{"data":[{"embedding":[1,2]}],"usage":{"prompt_tokens":23,"total_tokens":23}}`, 23, 0, map[string]string{"model": "client"}},
		{"zhipu_4v", Embeddings, `{"data":[{"embedding":[1,2]}],"usage":{"prompt_tokens":31,"total_tokens":31}}`, 31, 0, map[string]string{"model": "client"}},
		{"ali", Embeddings, `{"data":[{"embedding":"AACAPwAAAEA="}],"usage":{"total_tokens":9}}`, 9, 0, map[string]string{"model": "client", "data.0.embedding": "AACAPwAAAEA=", "usage.prompt_tokens": "9"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+string(tc.op), func(t *testing.T) {
			response, err := vectorAdapters()[tc.provider].Decode(context.Background(), &gateway.Request{Model: "client"}, gateway.Target{}, Input{Operation: tc.op}, vectorTestResponse(tc.body, 200))
			if err != nil {
				t.Fatal(err)
			}
			if response.Usage == nil || response.Usage.PromptTokens != tc.input || response.Usage.CompletionTokens != tc.output || response.Usage.Estimated {
				t.Fatalf("actual usage = %+v", response.Usage)
			}
			for key, want := range tc.checks {
				if got := gjson.GetBytes(response.Body, key).String(); got != want {
					t.Errorf("%s = %q, want %q; %s", key, got, want, response.Body)
				}
			}
			if response.Header.Get("Content-Length") != "" || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("stale converted headers: %v", response.Header)
			}
		})
	}
}

func TestVectorAbsentUsageIsNotActualZero(t *testing.T) {
	for _, tc := range []struct {
		provider string
		op       Operation
		body     string
	}{
		{"ollama", Embeddings, `{"embeddings":[[1,2]]}`},
		{"cohere", Rerank, `{"results":[{"index":0,"relevance_score":1}],"meta":{"billed_units":{"search_units":2}}}`},
		{"jina", Embeddings, `{"data":[{"embedding":[1]}]}`},
	} {
		response, err := vectorAdapters()[tc.provider].Decode(context.Background(), &gateway.Request{Model: "model"}, gateway.Target{}, Input{Operation: tc.op}, vectorTestResponse(tc.body, 200))
		if err != nil {
			t.Fatal(err)
		}
		if response.Usage != nil {
			t.Errorf("%s falsely claims actual usage: %+v", tc.provider, response.Usage)
		}
		if gjson.GetBytes(response.Body, "usage").Exists() {
			t.Errorf("%s added fake zero usage", tc.provider)
		}
		if tc.provider == "cohere" && gjson.GetBytes(response.Body, "meta.billed_units.search_units").Int() != 2 {
			t.Error("discarded actual Cohere search units")
		}
	}
	response, err := vectorAdapters()["ollama"].Decode(context.Background(), &gateway.Request{Model: "model"}, gateway.Target{}, Input{Operation: Embeddings}, vectorTestResponse(`{"embeddings":[[1]],"prompt_eval_count":0}`, 200))
	if err != nil || response.Usage == nil || response.Usage.PromptTokens != 0 {
		t.Fatalf("explicit upstream zero is actual: %+v %v", response.Usage, err)
	}
}

func TestVectorRejectsErrorsAndMalformedResponses(t *testing.T) {
	cases := []struct {
		provider string
		op       Operation
		status   int
		body     string
	}{
		{"ollama", Embeddings, 200, `{"error":"model unavailable"}`}, {"ollama", Embeddings, 200, `{"embeddings":[]}`}, {"ollama", Embeddings, 200, `{"embeddings":[[]]}`}, {"ollama", Embeddings, 200, `{"embeddings":[[1]],"prompt_eval_count":-1}`},
		{"baidu", Embeddings, 200, `{"error_code":110,"error_msg":"invalid token"}`}, {"ali", Rerank, 200, `{"code":"InvalidParameter","message":"bad query"}`}, {"cloudflare", Embeddings, 200, `{"success":false,"errors":[{"code":42}]}`},
		{"cohere", Rerank, 200, `{"results":[{"index":0,"relevance_score":1}],"meta":{"billed_units":{"input_tokens":-1}}}`}, {"cohere", Rerank, 200, `{"results":[{"index":-1,"relevance_score":1}]}`}, {"cohere", Rerank, 200, `{"results":[{"index":0.5,"relevance_score":1}]}`},
		{"jina", Embeddings, 200, `{"data":[{"embedding":[1,"bad"]}]}`}, {"jina", Embeddings, 200, `{"data":[{"embedding":null}]}`}, {"jina", Embeddings, 200, `{"data":[]}`}, {"jina", Embeddings, 200, `{"data":[{}]}`}, {"jina", Embeddings, 200, `{"data":[{"embedding":[1]}],"usage":{"total_tokens":-1}}`},
		{"volcengine", Embeddings, 429, `{"error":{"message":"rate limit"}}`}, {"mokaai", Embeddings, 200, `{"data":`}, {"zhipu_4v", Embeddings, 200, `[]`},
	}
	for _, tc := range cases {
		_, err := vectorAdapters()[tc.provider].Decode(context.Background(), &gateway.Request{Model: "model"}, gateway.Target{}, Input{Operation: tc.op}, vectorTestResponse(tc.body, tc.status))
		if err == nil {
			t.Errorf("%s accepted invalid response: %s", tc.provider, tc.body)
		}
	}
}

type vectorBrokenBody struct{ closed bool }

func (*vectorBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b *vectorBrokenBody) Close() error           { b.closed = true; return nil }

func TestVectorResponseReadFailureClosesBody(t *testing.T) {
	body := &vectorBrokenBody{}
	_, err := vectorAdapters()["ollama"].Decode(context.Background(), &gateway.Request{}, gateway.Target{}, Input{Operation: Embeddings}, &http.Response{StatusCode: 200, Body: body})
	if err == nil || !body.closed {
		t.Fatalf("read failure: %v closed:%v", err, body.closed)
	}
}
