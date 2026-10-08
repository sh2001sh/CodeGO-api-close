package live

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"mime"
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

func filesTestPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFilesContentAndSignedDeliveryIsolateActiveContent(t *testing.T) {
	store := filesTestStore(t)
	key := bytes.Repeat([]byte{0x42}, 32)
	mux := filesTestMux(t, store, 1024, key)
	for _, test := range []struct {
		name, declared, raw, disposition, contentType string
	}{
		{"html", "text/html", `<html><script>fetch('/api/user/self')</script></html>`, "attachment", "text/html"},
		{"svg", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`, "attachment", "image/svg+xml"},
		{"spoofed png", "image/png", `<html><script>alert(1)</script></html>`, "attachment", "image/png"},
		{"png", "application/octet-stream", string(filesTestPNG(t)), "inline", "image/png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := store.Create(context.Background(), 11, "test.bin", "user_data", test.declared, strings.NewReader(test.raw), 1024)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := BuildSignedFileDeliveryURL("https://app.test", file.ID, key, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(signed)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/v1/files/" + file.ID + "/content", u.RequestURI()} {
				auth := "owner"
				if strings.Contains(path, "/delivery") {
					auth = ""
				}
				w := filesTestRequest(mux, http.MethodGet, path, auth, nil, "")
				disposition, _, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
				if w.Code != 200 || err != nil || disposition != test.disposition || w.Header().Get("Content-Type") != test.contentType || w.Body.String() != test.raw {
					t.Fatalf("unsafe or corrupted delivery: status=%d headers=%v", w.Code, w.Header())
				}
				if w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(w.Header().Get("Content-Security-Policy"), "sandbox;") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
					t.Fatalf("missing isolation: %v", w.Header())
				}
			}
		})
	}
}

func TestAttachmentsImageReferenceRejectsActiveAndMislabeledFilesBeforeUploadOrSigning(t *testing.T) {
	store := filesTestStore(t)
	var uploads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { uploads.Add(1); w.WriteHeader(500) }))
	defer upstream.Close()
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	h.cfg.DeliveryKey = bytes.Repeat([]byte{0x31}, 32)
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://app.test")
	for _, fileType := range []struct{ declared, data string }{
		{"text/html", "<html><script>alert(1)</script></html>"},
		{"image/png", "<html><script>alert(1)</script></html>"},
		{"image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"/>`},
		{"image/jpeg", string(filesTestPNG(t))},
	} {
		file, err := store.Create(context.Background(), 11, "test.bin", "user_data", fileType.declared, strings.NewReader(fileType.data), 1024)
		if err != nil {
			t.Fatal(err)
		}
		for _, shape := range []struct {
			protocol gateway.Protocol
			body     string
		}{
			{gateway.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"image_url","image_url":{"file_id":%q}}]}]}`},
			{gateway.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"image_url","file_id":%q}]}]}`},
			{gateway.ProtocolResponses, `{"input":[{"type":"input_image","file_id":%q}]}`},
		} {
			req := attachmentsRequest(file.ID, shape.protocol)
			req.Body = []byte(fmt.Sprintf(shape.body, file.ID))
			for _, target := range []gateway.Target{attachmentsTarget(upstream.URL), {Provider: "custom"}} {
				if body, err := h.PrepareFileReferences(context.Background(), req, target); err == nil || len(body) != 0 {
					t.Fatalf("active/mislabeled image reached upstream: MIME=%s body=%s error=%v", fileType.declared, body, err)
				}
			}
		}
	}
	if uploads.Load() != 0 {
		t.Fatalf("invalid images caused %d native uploads", uploads.Load())
	}
}

func TestAttachmentsValidImageCanUseSignedDelivery(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, filesTestPNG(t))
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	h.cfg.DeliveryKey = bytes.Repeat([]byte{0x31}, 32)
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://app.test")
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	req.Body = []byte(fmt.Sprintf(`{"input":[{"type":"input_image","file_id":%q}]}`, file.ID))
	body, err := h.PrepareFileReferences(context.Background(), req, gateway.Target{Provider: "custom"})
	if err != nil || !strings.HasPrefix(gjson.GetBytes(body, "input.0.image_url").Str, "https://app.test/v1/files/"+file.ID+"/delivery?") {
		t.Fatalf("valid signed image broken: %s %v", body, err)
	}
}

func TestAttachmentsInvalidImagePreventsAllNativeUploadsInMixedRequest(t *testing.T) {
	store := filesTestStore(t)
	document := filesTestCreate(t, store, 11, []byte("a valid arbitrary document"))
	active := filesTestCreate(t, store, 11, []byte("<script>alert(1)</script>"))
	var uploads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		uploads.Add(1)
		_, _ = w.Write([]byte(`{"id":"file-native"}`))
	}))
	defer upstream.Close()
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	req := attachmentsRequest(document.ID, gateway.ProtocolResponses)
	req.Body = []byte(fmt.Sprintf(`{"input":[{"type":"input_file","file_id":%q},{"type":"input_image","file_id":%q}]}`, document.ID, active.ID))
	if body, err := h.PrepareFileReferences(context.Background(), req, attachmentsTarget(upstream.URL)); err == nil || len(body) != 0 || uploads.Load() != 0 {
		t.Fatalf("partially uploaded rejected request: body=%s err=%v uploads=%d", body, err, uploads.Load())
	}
}
