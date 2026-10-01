package auxiliary

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestMultipartFileAndModelMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		if r.FormValue("model") != "mapped" || r.FormValue("language") != "zh" {
			t.Errorf("bad form %v", r.Form)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = file.Close() }()
		data, _ := io.ReadAll(file)
		if header.Filename != "speech.wav" || !bytes.Equal(data, []byte{0, 1, 2, 255}) {
			t.Errorf("file changed %q %v", header.Filename, data)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "recognized speech")
	}))
	defer server.Close()
	h, _, settle, limits := testHandler(t, server.URL)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "alias")
	_ = writer.WriteField("language", "zh")
	_ = writer.WriteField("response_format", "text")
	file, _ := writer.CreateFormFile("file", "speech.wav")
	_, _ = file.Write([]byte{0, 1, 2, 255})
	_ = writer.Close()
	r := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Authorization", "Bearer client")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "recognized speech" || w.Header().Get("Content-Type") != "text/plain" || !settle.out.Usage.Estimated || !settle.out.Charge || limits.released != 1 {
		t.Fatalf("status=%d body=%s outcome=%+v", w.Code, w.Body.String(), settle.out)
	}
}

func TestBinarySpeechPreservesContentAndMarksEstimate(t *testing.T) {
	audio := []byte{0, 255, 4, 0, 3, 0}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(audio)
	}))
	defer server.Close()
	h, _, settle, _ := testHandler(t, server.URL)
	w := invoke(h, "/v1/audio/speech", `{"model":"tts","input":"你好","voice":"alloy","response_format":"pcm"}`)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), audio) || w.Header().Get("Content-Type") != "audio/pcm" || settle.out.Terminal != gateway.TerminalCompletedNoUsage || !settle.out.Charge || settle.out.Usage.PromptTokens != 2 {
		t.Fatalf("status=%d body=%v outcome=%+v", w.Code, w.Body.Bytes(), settle.out)
	}
}

func TestCompactAliasPlansBaseAndReservesVirtualModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/responses/compact" || strings.Contains(string(body), "-openai-compact") {
			t.Errorf("bad compact path=%s body=%s", r.URL.Path, body)
		}
		_, _ = io.WriteString(w, `{"output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":100,"output_tokens":10}}`)
	}))
	defer server.Close()
	h, plan, settle, _ := testHandler(t, server.URL)
	mux := http.NewServeMux()
	h.Register(mux)
	r := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses/compact", strings.NewReader(`{"model":"alias-openai-compact","input":"hello"}`))
	r.Header.Set("Authorization", "Bearer client")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || plan.model != "alias" || settle.model != "alias-openai-compact" || settle.out.Usage.PromptTokens != 100 {
		t.Fatalf("status=%d planned=%s reserved=%s outcome=%+v", w.Code, plan.model, settle.model, settle.out)
	}
}

func TestStreamingNeverRetriesAfterOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"text\":\"hello\"}]}\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"private upstream detail\"},\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\n")
	}))
	defer server.Close()
	var retries atomic.Int64
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { retries.Add(1) }))
	defer second.Close()
	h, _, settle, limits := testHandler(t, server.URL, second.URL)
	w := invoke(h, "/v1/completions", `{"model":"alias","prompt":"hello","stream":true}`)
	if strings.Contains(w.Body.String(), "private upstream detail") {
		t.Fatal("native error details leaked")
	}
	if !settle.out.Delivered || !settle.out.Charge || settle.out.Terminal != gateway.TerminalUpstreamErrorAfterOutput || settle.out.Usage.CompletionTokens != 2 || retries.Load() != 0 || limits.released != 1 {
		t.Fatalf("outcome=%+v retries=%d", settle.out, retries.Load())
	}
}

type disconnectedWriter struct{ header http.Header }

func (w disconnectedWriter) Header() http.Header       { return w.header }
func (w disconnectedWriter) WriteHeader(int)           {}
func (w disconnectedWriter) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

func TestClientWriteFailureDrainsActualUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"text\":\"hello\"}]}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(10 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	h, _, settle, _ := testHandler(t, server.URL)
	r := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(`{"model":"alias","prompt":"hello","stream":true}`))
	r.Header.Set("Authorization", "Bearer client")
	h.ServeHTTP(disconnectedWriter{make(http.Header)}, r)
	if settle.out.Terminal != gateway.TerminalClientCanceled || !settle.out.Charge || settle.out.Usage.CompletionTokens != 5 || !settle.finalized {
		t.Fatalf("outcome=%+v", settle.out)
	}
}

func TestCanceledClientDrainsResponseWithoutWriting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":12}}`)
	}))
	defer server.Close()
	h, _, settle, _ := testHandler(t, server.URL)
	r := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"alias","input":"hello"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer client")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if settle.out.Terminal != gateway.TerminalClientCanceled || !settle.out.Charge || settle.out.Usage.PromptTokens != 12 || w.Body.Len() != 0 {
		t.Fatalf("outcome=%+v body=%s", settle.out, w.Body.String())
	}
}

func TestGeminiWrapperLeavesChatToCore(t *testing.T) {
	h, _, _, _ := testHandler(t, "http://unused.invalid")
	var core atomic.Int64
	wrapped := h.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { core.Add(1); w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(`{}`)))
	if w.Code != 204 || core.Load() != 1 {
		t.Fatal("chat route was intercepted")
	}
}
