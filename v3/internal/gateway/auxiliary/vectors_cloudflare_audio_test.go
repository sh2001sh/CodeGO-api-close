package auxiliary

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func cloudflareAudioInput(t *testing.T, files ...[]byte) Input {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "client-model"); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		headers := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="speech.wav"`}, "Content-Type": {"audio/wav"}}
		part, err := writer.CreatePart(headers)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return Input{Operation: Transcriptions, ContentType: writer.FormDataContentType(), Body: body.Bytes()}
}

func TestCloudflareAudioNativeWire(t *testing.T) {
	for _, operation := range []Operation{Transcriptions, Translations} {
		t.Run(string(operation), func(t *testing.T) {
			audio := []byte{'R', 'I', 'F', 'F', 0, 1, 2, 3, 'W', 'A', 'V', 'E', 0xff, 0x00}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/client/v4/accounts/account/ai/run/@cf/openai/whisper" {
					t.Errorf("audio wire = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer provider-key" || r.Header.Get("Content-Type") != "audio/wav" {
					t.Errorf("audio headers = %v", r.Header)
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, audio) {
					t.Errorf("multipart leaked upstream, audio=%x", data)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"success":true,"result":{"text":"recognized speech","usage":{"input_tokens":10,"output_tokens":2}}}`)
			}))
			defer server.Close()
			req := &gateway.Request{Model: "client-model", Body: []byte(`{"model":"client-model","file_bytes":14,"response_format":"json"}`)}
			target := gateway.Target{BaseURL: server.URL, Secret: "account|provider-key", UpstreamModel: "@cf/openai/whisper"}
			in := cloudflareAudioInput(t, audio)
			in.Operation = operation
			adapter := defaultAdapters()["cloudflare"]
			wire, err := adapter.Build(context.Background(), req, target, in)
			if err != nil {
				t.Fatal(err)
			}
			upstream, err := server.Client().Do(wire)
			if err != nil {
				t.Fatal(err)
			}
			response, err := adapter.Decode(context.Background(), req, target, in, upstream)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || gjson.GetBytes(response.Body, "text").String() != "recognized speech" {
				t.Fatalf("audio response=%s calls=%d", response.Body, calls)
			}
			if response.Usage == nil || response.Usage.PromptTokens != 10 || response.Usage.CompletionTokens != 2 || response.Usage.Estimated {
				t.Errorf("actual audio usage=%+v", response.Usage)
			}
			if gjson.GetBytes(req.Body, "model").String() != "client-model" {
				t.Error("billing model mutated")
			}
		})
	}
}

func TestCloudflareAudioFormats(t *testing.T) {
	upstream := `{"result":{"text":"Hello world","language":"english","duration":1.25,"words":[{"word":"Hello","start":0,"end":0.5},{"word":"world","start":0.5,"end":1.25}]}}`
	for _, format := range []string{"json", "text", "verbose_json", "srt", "vtt"} {
		t.Run(format, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"response_format": format})
			if err != nil {
				t.Fatal(err)
			}
			req := &gateway.Request{Body: body}
			response, err := vectorAdapters()["cloudflare"].Decode(context.Background(), req, gateway.Target{}, Input{Operation: Translations}, vectorTestResponse(upstream, 200))
			if err != nil {
				t.Fatal(err)
			}
			if response.Usage != nil {
				t.Errorf("missing native usage became actual zero: %+v", response.Usage)
			}
			if response.Header.Get("Content-Length") != "" || response.Header.Get("Content-Encoding") != "" {
				t.Error("retained stale audio wrapper headers")
			}
			switch format {
			case "json":
				if gjson.GetBytes(response.Body, "text").String() != "Hello world" {
					t.Errorf("JSON=%s", response.Body)
				}
			case "text":
				if string(response.Body) != "Hello world" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") {
					t.Errorf("text=%s headers=%v", response.Body, response.Header)
				}
			case "verbose_json":
				if gjson.GetBytes(response.Body, "task").String() != "translate" || gjson.GetBytes(response.Body, "duration").Float() != 1.25 || gjson.GetBytes(response.Body, "words.1.word").String() != "world" {
					t.Errorf("verbose=%s", response.Body)
				}
			case "srt":
				if string(response.Body) != "1\n00:00:00,000 --> 00:00:00,500\nHello\n\n2\n00:00:00,500 --> 00:00:01,250\nworld\n\n" {
					t.Errorf("SRT=%s", response.Body)
				}
			case "vtt":
				if string(response.Body) != "WEBVTT\n\n00:00:00.000 --> 00:00:00.500\nHello\n\n00:00:00.500 --> 00:00:01.250\nworld\n\n" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/vtt") {
					t.Errorf("VTT=%s", response.Body)
				}
			}
		})
	}
	response, err := decodeCloudflareAudio(&gateway.Request{Body: []byte(`{"response_format":"verbose_json"}`)}, Input{Operation: Transcriptions}, vectorTestResponse(`{"result":{"text":"speech"}}`, 200))
	if err != nil || gjson.GetBytes(response.Body, "task").String() != "transcribe" || gjson.GetBytes(response.Body, "duration").Exists() {
		t.Fatalf("missing upstream duration should stay absent: %s %v", response.Body, err)
	}
}

func TestCloudflareAudioRejectsInvalidRequests(t *testing.T) {
	adapter := vectorAdapters()["cloudflare"]
	target := gateway.Target{BaseURL: "https://api.example", Secret: "account|key"}
	for _, in := range []Input{cloudflareAudioInput(t), cloudflareAudioInput(t, []byte{}), cloudflareAudioInput(t, []byte{1}, []byte{2}), {Operation: Transcriptions, ContentType: "multipart/form-data", Body: []byte("malformed")}, {Operation: Transcriptions, ContentType: "application/json", Body: []byte(`{"file":"url"}`)}} {
		_, err := adapter.Build(context.Background(), &gateway.Request{Model: "@cf/openai/whisper"}, target, in)
		if err == nil {
			t.Error("accepted missing/empty/duplicate/malformed audio")
		}
	}
	for _, model := range []string{"", "../model", "@cf/../model", "@cf//model", "@cf/model?secret=yes", "@cf/model%2fother"} {
		_, err := adapter.Build(context.Background(), &gateway.Request{Model: model}, target, cloudflareAudioInput(t, []byte{1}))
		if err == nil {
			t.Errorf("accepted invalid audio model %q", model)
		}
	}
	_, err := adapter.Build(context.Background(), &gateway.Request{Model: "@cf/openai/whisper", Body: []byte(`{"response_format":"unknown"}`)}, target, cloudflareAudioInput(t, []byte{1}))
	if err == nil {
		t.Error("accepted unknown audio response format")
	}
}

func TestCloudflareAudioRejectsInvalidNativeResponses(t *testing.T) {
	for _, body := range []string{`{`, `[]`, `{}`, `{"success":false,"result":{"text":"ignored"}}`, `{"error":{"message":"bad audio"}}`, `{"result":{"text":""}}`, `{"result":{"text":12}}`, `{"result":{"text":"   "}}`, `{"result":{"text":"speech"},"usage":{"prompt_tokens":-1}}`} {
		_, err := decodeCloudflareAudio(&gateway.Request{}, Input{Operation: Transcriptions}, vectorTestResponse(body, 200))
		if err == nil {
			t.Errorf("accepted invalid audio response %s", body)
		}
	}
	for _, body := range []string{`{"result":{"text":"speech"}}`, `{"result":{"text":"speech","words":[{"word":"speech","start":2,"end":1}]}}`, `{"result":{"text":"speech","words":[{"word":"speech","start":-1,"end":1}]}}`, `{"result":{"text":"speech","words":[{"word":"speech","start":0,"end":1000000}]}}`} {
		_, err := decodeCloudflareAudio(&gateway.Request{Body: []byte(`{"response_format":"srt"}`)}, Input{Operation: Transcriptions}, vectorTestResponse(body, 200))
		if err == nil {
			t.Errorf("accepted missing/malformed subtitle timing %s", body)
		}
	}
}

func TestCloudflareAudioGatewaySettlesOrRefunds(t *testing.T) {
	for _, tc := range []struct {
		name, upstream    string
		haveFile          bool
		status, calls     int
		charge, estimated bool
	}{
		{"actual success", `{"result":{"text":"speech"},"usage":{"prompt_tokens":12,"completion_tokens":2}}`, true, 200, 1, true, false},
		{"estimated success", `{"result":{"text":"speech"}}`, true, 200, 1, true, true},
		{"native error", `{"success":false,"errors":[{"message":"private-error"}]}`, true, 502, 1, false, false},
		{"empty text", `{"result":{"text":""}}`, true, 502, 1, false, false},
		{"malformed response", `{"result":`, true, 502, 1, false, false},
		{"missing file", `{"result":{"text":"unused"}}`, false, 400, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Set-Cookie", "provider-private=value")
				_, _ = io.WriteString(w, tc.upstream)
			}))
			defer server.Close()
			h, plan, settle, limits := testHandler(t, server.URL)
			plan.targets[0].Provider = "cloudflare"
			plan.targets[0].Secret = "account|key"
			plan.targets[0].UpstreamModel = "@cf/openai/whisper"
			in := cloudflareAudioInput(t)
			if tc.haveFile {
				in = cloudflareAudioInput(t, []byte{1, 2, 3})
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(in.Body))
			req.Header.Set("Content-Type", in.ContentType)
			req.Header.Set("Authorization", "Bearer client-key")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status || calls != tc.calls || !settle.finalized || settle.reserves != 1 || settle.out.Charge != tc.charge || limits.acquired != limits.released {
				t.Fatalf("status=%d calls=%d settlement=%+v limits=%+v", w.Code, calls, settle, limits)
			}
			if tc.charge && settle.out.Usage.Estimated != tc.estimated {
				t.Fatalf("usage=%+v", settle.out.Usage)
			}
			if w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "private-error") {
				t.Fatal("upstream private details escaped")
			}
		})
	}
}
