package live

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestAttachmentsAuthOverrideUsesInlineFileInsteadOfDifferentCredentialUpload(t *testing.T) {
	store := filesTestStore(t)
	file, err := store.Create(context.Background(), 11, "file.txt", "user_data", "text/plain", strings.NewReader("content"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer up.Close()
	h := attachmentsHandler(t, store, &attachmentsRepository{items: make(map[string]Locator)})
	target := attachmentsTarget(up.URL)
	target.HeaderOverride = map[string]string{"authorization": "Bearer overridden"}
	body, err := h.PrepareFileReferences(context.Background(), attachmentsRequest(file.ID, gateway.ProtocolResponses), target)
	if err != nil || calls.Load() != 0 || !strings.HasPrefix(gjson.GetBytes(body, "input.0.file_data").Str, "data:text/plain;base64,") {
		t.Fatalf("override used wrong native upload: %s calls=%d err=%v", body, calls.Load(), err)
	}
}

func TestAttachmentsInlineProtocolVariants(t *testing.T) {
	store := filesTestStore(t)
	file, err := store.Create(context.Background(), 11, "sample.pdf", "user_data", "application/pdf", bytes.NewReader([]byte{0, 128, 255}), 64)
	if err != nil {
		t.Fatal(err)
	}
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	dataURL := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString([]byte{0, 128, 255})
	for _, test := range []struct {
		name               string
		protocol           gateway.Protocol
		source, path, want string
	}{
		{"chat-file", gateway.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"file","file":{"file_id":%q}}]}]}`, "messages.0.content.0.file.file_data", dataURL},
		{"chat-image", gateway.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"image_url","image_url":{"file_id":%q,"detail":"high"}}]}]}`, "messages.0.content.0.image_url.url", dataURL},
		{"chat-direct-image", gateway.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"image_url","file_id":%q}]}]}`, "messages.0.content.0.image_url.url", dataURL},
		{"responses-file", gateway.ProtocolResponses, `{"input":[{"type":"input_file","file_id":%q}]}`, "input.0.file_data", dataURL},
		{"responses-image", gateway.ProtocolResponses, `{"input":[{"type":"input_image","file_id":%q}]}`, "input.0.image_url", dataURL},
		{"anthropic-source", gateway.ProtocolAnthropic, `{"messages":[{"content":[{"type":"document","source":{"type":"file","file_id":%q}}]}]}`, "messages.0.content.0.source.data", base64.StdEncoding.EncodeToString([]byte{0, 128, 255})},
		{"gemini-part", gateway.ProtocolGemini, `{"contents":[{"parts":[{"fileData":{"file_id":%q}}]}]}`, "contents.0.parts.0.inlineData.data", base64.StdEncoding.EncodeToString([]byte{0, 128, 255})},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := attachmentsRequest(file.ID, test.protocol)
			req.Body = []byte(fmt.Sprintf(test.source, file.ID))
			body, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"})
			if err != nil {
				t.Fatal(err)
			}
			if gjson.GetBytes(body, test.path).Str != test.want || bytes.Contains(body, []byte(file.ID)) {
				t.Fatalf("inline variant=%s", body)
			}
			if test.name == "anthropic-source" && (gjson.GetBytes(body, "messages.0.content.0.source.type").Str != "base64" || gjson.GetBytes(body, "messages.0.content.0.source.media_type").Str != "application/pdf") {
				t.Fatalf("Anthropic MIME/source=%s", body)
			}
			if test.name == "gemini-part" && (gjson.GetBytes(body, "contents.0.parts.0.inlineData.mimeType").Str != "application/pdf" || gjson.GetBytes(body, "contents.0.parts.0.fileData").Exists()) {
				t.Fatalf("Gemini MIME/source=%s", body)
			}
		})
	}
}

func TestAttachmentsSignedDeliveryAndInvalidConfiguration(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("document"))
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	h.cfg.DeliveryKey = bytes.Repeat([]byte{0x42}, 32)
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://delivery.test/v1")
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	body, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(gjson.GetBytes(body, "input.0.file_url").Str)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/v1/files/"+file.ID+"/delivery" || gjson.GetBytes(body, "input.0.file_id").Exists() || gjson.GetBytes(body, "input.0.file_data").Exists() {
		t.Fatalf("signed representation: %s", body)
	}
	if err := VerifyFileDeliveryToken(file.ID, u.Query().Get("expires"), u.Query().Get("signature"), h.cfg.DeliveryKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FILE_DELIVERY_BASE_URL", "file:///unsafe")
	if _, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"}); err == nil {
		t.Fatal("broken signer silently fell back")
	}
}

func TestAttachmentsBoundsUnsupportedAndPassThrough(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, bytes.Repeat([]byte{1}, 512))
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	h.cfg.MaxBodyBytes = 512
	if _, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"}); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("inline bound err=%v", err)
	}
	h.cfg.MaxBodyBytes = 1 << 20
	req.Body = []byte(strings.Repeat(`{"x":`, attachmentMaxDepth+1) + fmt.Sprintf(`{"type":"input_file","file_id":%q}`, file.ID) + strings.Repeat("}", attachmentMaxDepth+1))
	if _, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"}); err == nil {
		t.Fatal("deep request accepted")
	}
	req.Body = []byte(fmt.Sprintf(`{"unknown":{"file_id":%q}}`, file.ID))
	if _, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"}); err == nil {
		t.Fatal("unsupported reference shape accepted")
	}
	req.Body = []byte(`{"file_id":"file-external","counter":9007199254740993}`)
	body, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{})
	if err != nil || !bytes.Equal(body, req.Body) {
		t.Fatalf("external ID changed: %s %v", body, err)
	}
	req.Body = []byte(`{"text":"file-codego- is ordinary text","file_id":"file-external"}`)
	body, err = h.PrepareFileReferences(context.Background(), req, gateway.Target{})
	if err != nil || !bytes.Equal(body, req.Body) {
		t.Fatalf("ordinary text changed: %s %v", body, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = attachmentsRequest(file.ID, gateway.ProtocolResponses)
	if _, err := h.PrepareFileReferences(ctx, req, gateway.Target{Provider: "custom"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}
