package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func imageRequest(body string) *gateway.Request {
	return &gateway.Request{ID: "client-id", Received: time.Unix(123, 0), Model: "public-model", Body: []byte(body)}
}

func target(base string) gateway.Target {
	return gateway.Target{BaseURL: base, Secret: "test-key", UpstreamModel: "owner/model"}
}

func TestChatProtocolsRemainUnsupported(t *testing.T) {
	for _, protocol := range []gateway.Protocol{gateway.ProtocolOpenAIChat, gateway.ProtocolResponses, gateway.ProtocolAnthropic, gateway.ProtocolGemini} {
		request := imageRequest(`{"messages":[{"role":"user","content":"hello"}]}`)
		request.Protocol = protocol
		_, err := (Provider{}).BuildRequest(context.Background(), request, target("https://example.com"))
		var reported *gateway.UpstreamError
		if !errors.As(err, &reported) || reported.Code != "unsupported_protocol" || reported.Status != 400 {
			t.Fatalf("protocol %d invented support: %v", protocol, err)
		}
		stream := (Provider{}).Decode(request, &http.Response{Body: io.NopCloser(strings.NewReader(`{}`))})
		event, err := stream.Next()
		_ = stream.Close()
		if err != nil || event.Kind != gateway.EventError || event.Err.Code != "unsupported_protocol" {
			t.Fatalf("defensive decoder accepted protocol %d: %#v %v", protocol, event, err)
		}
	}
}

func TestBuildPredictionEndpointsAndNativeInput(t *testing.T) {
	for _, tc := range []struct{ model, path, version string }{
		{"owner/model", "/prefix/v1/models/owner/model/predictions", ""},
		{"owner/model:abc123", "/prefix/v1/predictions", "abc123"},
		{"abc123", "/prefix/v1/predictions", "abc123"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			chosen := target("https://upstream.example/prefix/v1/")
			chosen.UpstreamModel = tc.model
			request := imageRequest(`{"model":"public-model","prompt":"hello","input":{"seed":9007199254740993,"image_prompt":"https://images.example/source","nested":{"a":[1,true]}}}`)
			out, err := BuildImageRequest(context.Background(), request, chosen)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = out.Body.Close() }()
			if out.URL.Path != tc.path || out.Method != http.MethodPost || out.Header.Get("Authorization") != "Bearer test-key" || out.Header.Get("Prefer") != "wait" || out.Header.Get("Accept") != "application/json" {
				t.Fatalf("wrong native request: %s %s headers=%v", out.Method, out.URL, out.Header)
			}
			data, _ := io.ReadAll(out.Body)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, exists := fields["model"]; exists {
				t.Fatalf("client model leaked to native request: %s", data)
			}
			if tc.version != "" && string(fields["version"]) != `"`+tc.version+`"` {
				t.Fatalf("wrong version: %s", data)
			}
			if !strings.Contains(string(data), `9007199254740993`) || !strings.Contains(string(data), `"nested":{"a":[1,true]}`) {
				t.Fatalf("native input corrupted: %s", data)
			}
		})
	}
}

func TestPreservesFluxOptionsAndOverrideOrder(t *testing.T) {
	request := imageRequest(`{"prompt":"initial","size":"1792x1024","quality":"HD","output_format":"png","n":2,"response_format":"url","extra_fields":{"prompt":"extra","seed":7},"input":{"prompt":"final","seed":8},"seed":9,"image_prompt":"https://images.example/uploaded"}`)
	out, err := BuildImageRequest(context.Background(), request, target("https://example.com"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	var native struct {
		Input map[string]json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(out.Body).Decode(&native); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"prompt": `"final"`, "aspect_ratio": `"16:9"`, "num_outputs": "2", "prompt_upsampling": "true", "output_format": `"png"`, "seed": "9", "image_prompt": `"https://images.example/uploaded"`} {
		if string(native.Input[key]) != want {
			t.Errorf("input.%s = %s, want %s", key, native.Input[key], want)
		}
	}
	if _, exists := native.Input["response_format"]; exists {
		t.Error("response_format must not be sent as model input")
	}
}

func TestImageSizeMappingsAndBounds(t *testing.T) {
	for _, tc := range []struct {
		size, ratio   string
		width, height int
	}{
		{"1024x1024", "1:1", 0, 0}, {"1024x1792", "9:16", 0, 0}, {"1536x1024", "3:2", 0, 0},
		{"1024x1536", "2:3", 0, 0}, {"800x1000", "4:5", 0, 0}, {"999x777", "custom", 992, 768},
		{"1x9999", "custom", 256, 1440},
	} {
		request := imageRequest(fmt.Sprintf(`{"prompt":"hello","size":%q}`, tc.size))
		out, err := BuildImageRequest(context.Background(), request, target("https://example.com"))
		if err != nil {
			t.Fatal(err)
		}
		var native struct {
			Input struct {
				Ratio  string `json:"aspect_ratio"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"input"`
		}
		err = json.NewDecoder(out.Body).Decode(&native)
		_ = out.Body.Close()
		if err != nil || native.Input.Ratio != tc.ratio || native.Input.Width != tc.width || native.Input.Height != tc.height {
			t.Errorf("size %s got %#v err=%v", tc.size, native, err)
		}
	}
}

func TestBuildRejectsInvalidInput(t *testing.T) {
	for _, body := range []string{
		`{"prompt":`, `null`, `[]`, `{}`, `{"prompt":" "}`, `{"prompt":5}`,
		`{"prompt":"x","input":null}`, `{"prompt":"x","input":[]}`, `{"prompt":"x","extra_fields":[]}`,
		`{"prompt":"x","n":0}`, `{"prompt":"x","n":17}`, `{"prompt":"x","n":1.5}`,
		`{"prompt":"x","output_format":{}}`, `{"prompt":"x","image_prompt":[]}`, `{"prompt":"x","quality":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := BuildImageRequest(context.Background(), imageRequest(body), target("https://example.com"))
			var reported *gateway.UpstreamError
			if !errors.As(err, &reported) || reported.Status != 400 {
				t.Fatalf("expected safe 400, got %v", err)
			}
		})
	}
	for _, model := range []string{"../model", "owner/..", "owner/model/other", "owner/model?leak", "owner/model:", "owner/model:x/y"} {
		chosen := target("https://example.com")
		chosen.UpstreamModel = model
		if _, err := BuildImageRequest(context.Background(), imageRequest(`{"prompt":"hello"}`), chosen); err == nil {
			t.Errorf("accepted invalid model %q", model)
		}
	}
	for _, base := range []string{"ftp://example.com", "https://user:key@example.com", "https://example.com?q=x", "https://example.com/#fragment", "/relative"} {
		if _, err := BuildImageRequest(context.Background(), imageRequest(`{"prompt":"hello"}`), target(base)); err == nil {
			t.Errorf("accepted invalid base %q", base)
		}
	}
}

func TestInvalidChannelConfigurationAllowsFailoverWithoutLeaks(t *testing.T) {
	for _, tc := range []struct{ field, code string }{
		{"secret", "invalid_credentials"}, {"endpoint", "invalid_endpoint"}, {"proxy", "invalid_proxy"}, {"model", "invalid_model"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			chosen := target("https://example.com")
			switch tc.field {
			case "secret":
				chosen.Secret = ""
			case "endpoint":
				chosen.BaseURL = "https://private:credential@invalid.example"
			case "proxy":
				chosen.ProxyURL = "not-a-proxy-private"
			case "model":
				chosen.UpstreamModel = "private/model/invalid"
			}
			_, err := BuildImageRequest(context.Background(), imageRequest(`{"prompt":"hello"}`), chosen)
			var reported *gateway.UpstreamError
			if !errors.As(err, &reported) || reported.Status != http.StatusBadGateway || reported.Type != "upstream_error" || reported.Code != tc.code {
				t.Fatalf("configuration failure incorrectly blames caller: %v", err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "test-key") {
				t.Fatalf("configuration value leaked: %v", err)
			}
		})
	}
}

func TestCreateAndPollImageSuccessWithoutTokenUsage(t *testing.T) {
	var creates, polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing API credential")
		}
		switch r.URL.Path {
		case "/v1/models/owner/model/predictions":
			creates.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Prefer") != "wait" {
				t.Error("wrong create request")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"id":"job1","status":"starting","urls":{"get":"/v1/predictions/job1"}}`)
		case "/v1/predictions/job1":
			if r.Method != http.MethodGet {
				t.Error("poll was not GET")
			}
			if polls.Add(1) == 1 {
				_, _ = fmt.Fprint(w, `{"id":"job1","status":"processing"}`)
			} else {
				_, _ = fmt.Fprint(w, `{"id":"job1","status":"succeeded","output":["https://images.example/1.png","https://images.example/2.png"],"metrics":{"predict_time":0.2}}`)
			}
		default:
			t.Errorf("unexpected URL: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	stream := run(t, context.Background(), Provider{PollInterval: time.Millisecond}, imageRequest(`{"prompt":"hello"}`), target(server.URL))
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil || event.Kind != gateway.EventData || event.Usage != nil || event.TextBytes != 0 {
		t.Fatalf("bad completion: %#v err=%v", event, err)
	}
	var result struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(event.Payload, &result) != nil || result.Created <= 0 || len(result.Data) != 2 || result.Data[0].URL != "https://images.example/1.png" || result.Data[1].URL != "https://images.example/2.png" || strings.Contains(string(event.Payload), `"usage"`) {
		t.Fatalf("bad image JSON: %s", event.Payload)
	}
	if creates.Load() != 1 || polls.Load() != 2 {
		t.Fatalf("create/poll count %d/%d", creates.Load(), polls.Load())
	}
	if _, err = stream.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestPredictionFailuresAndMalformedResponses(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{`{"status":"failed","error":"model failed"}`, "prediction_failed"},
		{`{"status":"failed","error":{"detail":"model failed"}}`, "prediction_failed"},
		{`{"status":"failed"}`, "prediction_failed"},
		{`{"status":"canceled","error":"canceled by owner"}`, "prediction_canceled"},
		{`{"error":{"message":"API error"}}`, "prediction_failed"},
		{`{"status":"starting"}`, "invalid_response"},
		{`{"status":"succeeded","output":null}`, "empty_response"},
		{`{"status":"succeeded","output":[]}`, "empty_response"},
		{`{"status":"succeeded","output":{"unexpected":"shape"}}`, "invalid_response"},
		{`{"status":"succeeded","output":["https://images.example/1",1]}`, "invalid_response"},
		{`{"status":"succeeded","output":"file:///private"}`, "invalid_response"},
		{`{"status":"succeeded","output":`, "invalid_response"},
		{`[]`, "invalid_response"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			stream := DecodeImages(imageRequest(`{"prompt":"hello"}`), &http.Response{Body: io.NopCloser(strings.NewReader(tc.body))})
			defer func() { _ = stream.Close() }()
			event, err := stream.Next()
			if err != nil || event.Kind != gateway.EventError || event.Err == nil || event.Err.Code != tc.code {
				t.Fatalf("expected %s, got %#v err=%v", tc.code, event, err)
			}
			if _, err := stream.Next(); err != io.EOF {
				t.Fatalf("error stream continued: %v", err)
			}
		})
	}
}

func run(t *testing.T, ctx context.Context, p Provider, request *gateway.Request, chosen gateway.Target) gateway.EventStream {
	t.Helper()
	out, err := BuildImageRequest(ctx, request, chosen)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.RoundTrip(out)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("unexpected create HTTP status: %d", resp.StatusCode)
	}
	return DecodeImages(request, resp)
}
