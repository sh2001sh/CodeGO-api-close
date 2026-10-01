package auxiliary

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func mediaTestResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "Authorization": {"native-secret"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestMediaImageConversionCountAndNativeUsage(t *testing.T) {
	cases := []struct {
		provider, body     string
		count              int
		prompt, completion int64
	}{
		{"minimax", `{"base_resp":{"status_code":0},"data":{"image_urls":["https://example.com/a"],"image_base64":["YmI=","Y2M="]}}`, 3, 0, 0},
		{"volcengine", `{"data":[{"url":"https://example.com/a"},{"b64_json":"YmI="}],"usage":{"input_tokens":5,"output_tokens":30,"output_tokens_details":{"image_tokens":30}}}`, 2, 5, 30},
		{"jimeng", `{"code":10000,"data":{"image_urls":["https://example.com/a"],"binary_data_base64":["YmI="]}}`, 2, 0, 0},
		{"ali", `{"output":{"choices":[{"message":{"content":[{"image":"YmI="},{"image":"Y2M="},{"text":"better prompt"}]}}]},"usage":{"input_tokens":3,"output_tokens":9}}`, 2, 3, 9},
		{"zhipu_4v", `{"data":[{"b64_image":"YmI="}],"usage":{"prompt_tokens":2,"completion_tokens":4}}`, 1, 2, 4},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			req := &gateway.Request{Model: "qwen-image", Body: []byte(`{"model":"qwen-image"}`), Received: time.Unix(1700000000, 0)}
			response, err := mediaAdapters()[tc.provider].Decode(context.Background(), req, gateway.Target{Secret: "native-secret"}, Input{Operation: Images}, mediaTestResponse(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(gjson.GetBytes(response.Body, "data").Array()); got != tc.count {
				t.Fatalf("images %d want %d", got, tc.count)
			}
			if response.Header.Get("X-Codego-Image-Count") != strconv.Itoa(tc.count) {
				t.Errorf("count header %q", response.Header.Get("X-Codego-Image-Count"))
			}
			if response.Header.Get("Authorization") != "" || bytes.Contains(response.Body, []byte("native-secret")) {
				t.Error("native credentials exposed")
			}
			if gjson.GetBytes(response.Body, "created").Int() != 1700000000 {
				t.Error("creation time changed")
			}
			if tc.prompt != 0 && (response.Usage == nil || response.Usage.PromptTokens != tc.prompt || response.Usage.CompletionTokens != tc.completion || response.Usage.Estimated) {
				t.Fatalf("usage %+v", response.Usage)
			}
		})
	}
}

func TestMediaSpeechBinaryFormatAndCharacterAccounting(t *testing.T) {
	cases := []struct {
		provider, body, format, contentType string
		prompt                              int64
		estimated                           bool
	}{
		{"minimax", `{"base_resp":{"status_code":0},"data":{"audio":"000102ff"},"extra_info":{"usage_characters":4}}`, "wav", "audio/wav", 4, false},
		{"minimax", `{"base_resp":{"status_code":0},"data":{"audio":"000102ff"}}`, "flac", "audio/flac", 2, true},
		{"volcengine", `{"code":3000,"data":"AAEC/w=="}`, "opus", "audio/ogg", 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+tc.format, func(t *testing.T) {
			req := &gateway.Request{Body: []byte(`{"model":"tts","input":"你好","response_format":"` + tc.format + `"}`)}
			response, err := mediaAdapters()[tc.provider].Decode(context.Background(), req, gateway.Target{}, Input{Operation: Speech}, mediaTestResponse(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(response.Body, []byte{0, 1, 2, 255}) || response.Header.Get("Content-Type") != tc.contentType {
				t.Fatalf("audio %v content %s", response.Body, response.Header.Get("Content-Type"))
			}
			if response.Usage == nil || response.Usage.PromptTokens != tc.prompt || response.Usage.Estimated != tc.estimated {
				t.Fatalf("usage %+v", response.Usage)
			}
		})
	}
}

func TestMediaNativeFailureAndEmptyOutputAreRejected(t *testing.T) {
	cases := []struct {
		provider string
		op       Operation
		body     string
	}{
		{"minimax", Speech, `{"base_resp":{"status_code":1001,"status_msg":"native-secret"}}`},
		{"minimax", Speech, `{"data":{"audio":"zz"}}`},
		{"minimax", Speech, `{"data":{"audio":"0102"},"extra_info":{"usage_characters":-1}}`},
		{"minimax", Images, `{"data":{"image_urls":[]}}`},
		{"volcengine", Speech, `{"code":3001,"message":"native-secret"}`},
		{"volcengine", Speech, `{"code":3000,"data":"bad-base64"}`},
		{"volcengine", Images, `{"error":{"message":"native-secret"}}`},
		{"volcengine", Images, `{"data":[{}]}`},
		{"volcengine", Images, `{"data":[{"url":"https://example.com/a"}],"usage":{"input_tokens":-1,"output_tokens":5}}`},
		{"jimeng", Images, `{"code":10001,"message":"native-secret"}`},
		{"ali", Images, `{"code":"InvalidApiKey","message":"native-secret"}`},
		{"zhipu_4v", Images, `{"error":{"message":"native-secret"}}`},
		{"zhipu_4v", Images, `{"data":[{}]}`},
		{"minimax", Images, `garbage`},
	}
	for _, tc := range cases {
		_, err := mediaAdapters()[tc.provider].Decode(context.Background(), &gateway.Request{Model: "qwen-image"}, gateway.Target{Secret: "native-secret"}, Input{Operation: tc.op}, mediaTestResponse(tc.body))
		if err == nil || strings.Contains(err.Error(), "native-secret") {
			t.Errorf("%s unsafe error: %v", tc.provider, err)
		}
	}
}

func TestZhipuMediaDownloadsBase64WithoutProviderCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image.png" || r.URL.Query().Get("signature") != "cdn-signature" {
			t.Errorf("download URL %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("provider credential sent to CDN")
		}
		_, _ = w.Write([]byte{0, 1, 2, 255})
	}))
	defer server.Close()
	data := `{"data":[{"image_url":"` + server.URL + `/image.png?signature=cdn-signature"}]}`
	response, err := mediaAdapters()["zhipu_4v"].Decode(context.Background(), &gateway.Request{}, gateway.Target{Secret: "native-secret"}, Input{Operation: Images}, mediaTestResponse(data))
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(response.Body, "data.0.b64_json").String() != "AAEC/w==" || gjson.GetBytes(response.Body, "data.0.url").Exists() {
		t.Fatalf("output %s", response.Body)
	}
}

func TestMediaSpeechAccountsNativeMetadataTextOverride(t *testing.T) {
	for _, tc := range []struct{ provider, body, native string }{
		{"minimax", `{"input":"a","metadata":{"text":"你好世界"}}`, `{"base_resp":{"status_code":0},"data":{"audio":"0102"}}`},
		{"volcengine", `{"input":"a","metadata":{"request":{"text":"你好世界"}}}`, `{"code":3000,"data":"AQI="}`},
	} {
		response, err := mediaAdapters()[tc.provider].Decode(context.Background(), &gateway.Request{Body: []byte(tc.body)}, gateway.Target{}, Input{Operation: Speech}, mediaTestResponse(tc.native))
		if err != nil {
			t.Fatal(err)
		}
		if response.Usage.PromptTokens != 4 || response.Header.Get("X-Codego-Audio-Characters") != "4" {
			t.Fatalf("%s native text undercounted: %+v", tc.provider, response)
		}
	}
}
