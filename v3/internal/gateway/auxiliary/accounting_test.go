package auxiliary

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type pricedSettle struct {
	price        catalog.Price
	held, actual credits.Micro
	out          gateway.Outcome
	finalErr     error
}

func (s *pricedSettle) Reserve(_ context.Context, req *gateway.Request) error {
	u, err := pricing.EstimateUsageForPrice(req.Body, s.price, pricing.EstimateConfig{})
	if err != nil {
		return err
	}
	s.held, err = pricing.Price(u, s.price, 1)
	return err
}

func (s *pricedSettle) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	s.out = out
	if out.Charge {
		s.actual, s.finalErr = pricing.Price(out.Usage, s.price, 1)
	}
	return s.finalErr
}

func mediaPrice(unit string, amount int64) catalog.Price {
	return catalog.Price{Mode: "per_request", PerRequest: amount, Rules: map[string]any{"billing_unit": unit}}
}

func TestImageUnitsReserveRequestedAndSettleProduced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"YQ=="},{"b64_json":"Yg=="}],"usage":{"prompt_tokens":9,"completion_tokens":8}}`)
	}))
	defer server.Close()
	h, _, _, _ := testHandler(t, server.URL)
	s := &pricedSettle{price: mediaPrice("image", 100)}
	h.cfg.Settler = s
	w := invoke(h, "/v1/images/generations", `{"model":"image","prompt":"cats","n":3}`)
	if w.Code != 200 || s.held != 300 || s.actual != 200 || s.finalErr != nil || s.out.Usage.ImageCount != 2 || s.out.Usage.CompletionTokens != 8 {
		t.Fatalf("status=%d held=%d actual=%d err=%v outcome=%+v", w.Code, s.held, s.actual, s.finalErr, s.out)
	}
}

func TestSpeechUnitsCountUnicodeCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte{0, 0, 1, 1})
	}))
	defer server.Close()
	h, _, _, _ := testHandler(t, server.URL)
	s := &pricedSettle{price: mediaPrice("audio_character", 15)}
	h.cfg.Settler = s
	w := invoke(h, "/v1/audio/speech", `{"model":"tts","input":"汉字🙂é","voice":"alloy","response_format":"pcm"}`)
	if w.Code != 200 || s.held != 60 || s.actual != 60 || s.finalErr != nil || s.out.Usage.AudioCharacters != 4 {
		t.Fatalf("status=%d held=%d actual=%d err=%v outcome=%+v", w.Code, s.held, s.actual, s.finalErr, s.out)
	}
}

func testWAV() []byte {
	audio := make([]byte, 44+20000)
	copy(audio, "RIFF")
	binary.LittleEndian.PutUint32(audio[4:8], uint32(len(audio)-8))
	copy(audio[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(audio[16:20], 16)
	binary.LittleEndian.PutUint16(audio[20:22], 1)
	binary.LittleEndian.PutUint16(audio[22:24], 1)
	binary.LittleEndian.PutUint32(audio[24:28], 8000)
	binary.LittleEndian.PutUint32(audio[28:32], 1_000_000_000) // Cannot understate duration with forged ByteRate.
	binary.LittleEndian.PutUint16(audio[32:34], 2)
	binary.LittleEndian.PutUint16(audio[34:36], 16)
	copy(audio[36:], "data")
	binary.LittleEndian.PutUint32(audio[40:44], 20000)
	return audio
}

func audioUpload(t *testing.T, extra string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", "speech"); err != nil {
		t.Fatal(err)
	}
	file, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(testWAV()); err != nil {
		t.Fatal(err)
	}
	if extra != "" {
		if err = w.WriteField(extra, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}

func TestAudioUnitsUseMeasuredUploadDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"text":"recognized","usage":{"prompt_tokens":999,"total_tokens":999}}`)
	}))
	defer server.Close()
	h, _, _, _ := testHandler(t, server.URL)
	s := &pricedSettle{price: mediaPrice("audio_second", 400)}
	h.cfg.Settler = s
	body, contentType := audioUpload(t, "")
	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Authorization", "Bearer client")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || s.held != 500 || s.actual != 500 || s.finalErr != nil || s.out.Usage.AudioDurationMicros != 1_250_000 || s.out.Usage.PromptTokens != 999 {
		t.Fatalf("status=%d held=%d actual=%d err=%v outcome=%+v", w.Code, s.held, s.actual, s.finalErr, s.out)
	}
}

func TestClientCannotOverrideMeasuredMultipartAccounting(t *testing.T) {
	for _, field := range []string{"audio_duration_micros", "duration", "file_bytes"} {
		t.Run(field, func(t *testing.T) {
			h, _, s, _ := testHandler(t, "http://unused.invalid")
			body, contentType := audioUpload(t, field)
			r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", body)
			r.Header.Set("Content-Type", contentType)
			r.Header.Set("Authorization", "Bearer client")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 400 || s.reserves != 0 {
				t.Fatalf("status=%d reserves=%d", w.Code, s.reserves)
			}
		})
	}
}

func TestUsageOnlySSEIsEmptyAndRefunded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	h, _, s, _ := testHandler(t, server.URL)
	w := invoke(h, "/v1/completions", `{"model":"alias","prompt":"hello","stream":true}`)
	if w.Code != 502 || s.out.Terminal != gateway.TerminalEmptyStream || s.out.Charge || s.out.Delivered {
		t.Fatalf("status=%d outcome=%+v", w.Code, s.out)
	}
}

func TestNativeUsageSerializationPreservesEstimateAndUnits(t *testing.T) {
	usage := gateway.Usage{PromptTokens: 2, CompletionTokens: 3, Estimated: true, ImageCount: 4, AudioCharacters: 5, AudioDurationMicros: 6, VideoDurationMicros: 7, CacheWriteTokens: 8}
	parsed := parseUsage(usageJSON(usage))
	if parsed == nil || !reflect.DeepEqual(*parsed, usage) {
		t.Fatalf("round trip=%+v want=%+v", parsed, usage)
	}
}
