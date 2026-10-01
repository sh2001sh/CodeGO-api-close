package auxiliary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestMediaNativePayloadPathAndAuthentication(t *testing.T) {
	cases := []struct {
		provider                        string
		op                              Operation
		body, model, secret, path, auth string
		fields                          map[string]string
	}{
		{"minimax", Images, `{"model":"alias","prompt":"paint","n":2,"size":"1792x1024","response_format":"b64_json","watermark":false}`, "image-01", "native-secret", "/v1/image_generation", "Bearer native-secret", map[string]string{"model": "image-01", "aspect_ratio": "16:9", "response_format": "base64", "n": "2", "aigc_watermark": "false"}},
		{"minimax", Speech, `{"model":"alias","input":"Hello","voice":"speaker","response_format":"wav","metadata":{"voice_setting":{"vol":2},"model":"attacker","stream":true}}`, "speech-02-hd", "native-secret", "/v1/t2a_v2", "Bearer native-secret", map[string]string{"model": "speech-02-hd", "text": "Hello", "voice_setting.voice_id": "speaker", "voice_setting.vol": "2", "audio_setting.format": "wav", "output_format": "hex", "stream": "false"}},
		{"volcengine", Images, `{"model":"alias","prompt":"paint","n":2,"size":"1024x1024","watermark":false}`, "ep-image", "native-secret", "/api/v3/images/generations", "Bearer native-secret", map[string]string{"model": "ep-image", "size": "1024x1024", "watermark": "false"}},
		{"volcengine", Speech, `{"model":"alias","input":"Hello","voice":"nova","response_format":"opus","metadata":{"app":{"appid":"attacker","token":"attacker"},"audio":{"rate":16000}}}`, "tts-model", "native-app|native-token", "/v1/audio/speech", "Bearer;native-token", map[string]string{"app.appid": "native-app", "app.token": "native-token", "app.cluster": "volcano_tts", "audio.voice_type": "zh_female_shuangkuaisisi_mars_bigtts", "audio.encoding": "ogg_opus", "audio.rate": "16000", "request.operation": "query", "request.model": "tts-model"}},
		{"ali", Images, `{"model":"alias","prompt":"paint","n":2,"size":"1024x768"}`, "qwen-image", "native-secret", "/api/v1/services/aigc/multimodal-generation/generation", "Bearer native-secret", map[string]string{"model": "qwen-image", "parameters.size": "1024*768", "parameters.n": "2", "input.messages.0.content.0.text": "paint"}},
		{"ali", Images, `{"model":"alias","prompt":"paint","parameters":{"n":3,"prompt_extend":true},"input":{"prompt":"native"}}`, "wanx-v1", "native-secret", "/api/v1/services/aigc/text2image/image-synthesis", "Bearer native-secret", map[string]string{"model": "wanx-v1", "parameters.n": "3", "parameters.prompt_extend": "true", "input.prompt": "native"}},
		{"zhipu_4v", Images, `{"model":"alias","prompt":"paint","quality":"hd","watermark_enabled":false}`, "cogview-4", "native-secret", "/api/paas/v4/images/generations", "Bearer native-secret", map[string]string{"model": "cogview-4", "quality": "hd", "watermark_enabled": "false"}},
		{"jimeng", Images, `{"model":"alias","prompt":"paint","response_format":"b64_json","extra_fields":{"width":768,"seed":42,"req_key":"attacker"}}`, "jimeng_high_aes_general_v21_L", "access-key|signing-secret", "/", "", map[string]string{"req_key": "jimeng_high_aes_general_v21_L", "width": "768", "seed": "42", "return_url": "false"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+string(tc.op), func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != tc.path {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				if tc.auth != "" && r.Header.Get("Authorization") != tc.auth {
					t.Errorf("wrong auth scheme")
				}
				body, _ := io.ReadAll(r.Body)
				for field, expected := range tc.fields {
					if got := gjson.GetBytes(body, field).String(); got != expected {
						t.Errorf("%s = %q, want %q", field, got, expected)
					}
				}
				if tc.provider == "jimeng" {
					if r.URL.Query().Get("Action") != "CVProcess" || r.URL.Query().Get("Version") != "2022-08-31" {
						t.Error("wrong native query")
					}
					hash := sha256.Sum256(body)
					if r.Header.Get("X-Content-Sha256") != hex.EncodeToString(hash[:]) || !strings.HasPrefix(r.Header.Get("Authorization"), "HMAC-SHA256 Credential=access-key/") {
						t.Error("unsigned image payload")
					}
				}
				if tc.provider == "ali" && tc.model == "wanx-v1" && r.Header.Get("X-DashScope-Async") != "enable" {
					t.Error("missing async header")
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			req := &gateway.Request{ID: "request-id", Model: "alias", Body: []byte(tc.body)}
			target := gateway.Target{BaseURL: server.URL, Secret: tc.secret, UpstreamModel: tc.model}
			upstream, err := mediaAdapters()[tc.provider].Build(context.Background(), req, target, Input{Operation: tc.op, Body: req.Body, ContentType: "application/json"})
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(upstream)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if calls != 1 {
				t.Fatalf("calls %d", calls)
			}
		})
	}
}

func TestMediaUnsupportedAndInvalidCredentials(t *testing.T) {
	for provider, adapter := range mediaAdapters() {
		_, err := adapter.Build(context.Background(), &gateway.Request{Model: "x", Body: []byte(`{"model":"x"}`)}, gateway.Target{}, Input{Operation: Moderations})
		if err == nil {
			t.Errorf("%s accepted unsupported operation", provider)
		}
	}
	for _, provider := range []string{"volcengine", "jimeng"} {
		op := Speech
		if provider == "jimeng" {
			op = Images
		}
		_, err := mediaAdapters()[provider].Build(context.Background(), &gateway.Request{Model: "x", Body: []byte(`{"model":"x"}`)}, gateway.Target{Secret: "invalid-native-secret"}, Input{Operation: op})
		if err == nil || strings.Contains(err.Error(), "invalid-native-secret") {
			t.Errorf("%s credential validation unsafe: %v", provider, err)
		}
	}
}

func TestJimengSignatureGolden(t *testing.T) {
	r, err := jsonRequest(context.Background(), "https://visual.volcengineapi.com/?Action=CVProcess&Version=2022-08-31", "", map[string]any{"prompt": "cat", "req_key": "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := signJimengMedia(r, "AKID|SECRET", time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// Independently computed with Python hashlib/hmac from the documented canonical string.
	const want = "HMAC-SHA256 Credential=AKID/20260930/cn-north-1/cv/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=1fa5f51cfa9f0ba53fdcd1913cfba70da53e9cdc7452f0165d0769f0ce20810a"
	if got := r.Header.Get("Authorization"); got != want {
		t.Fatalf("signature = %s", got)
	}
}
