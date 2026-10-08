package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestAttachmentsNativeMultipartPersistenceAndDeletion(t *testing.T) {
	store := filesTestStore(t)
	raw := filesTestPNG(t)
	file, err := store.Create(context.Background(), 11, "图像.bin", "vision", "image/png", bytes.NewReader(raw), 1024)
	if err != nil {
		t.Fatal(err)
	}
	var uploads atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads.Add(1)
		if r.Method != "POST" || r.URL.Path != "/nested/v1/files" || r.URL.Query().Get("version") != "1" {
			t.Errorf("upload endpoint: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer test-upstream-secret" || r.Header.Get("Cookie") != "" {
			t.Errorf("upload headers: %v", r.Header)
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		part, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer func() { _ = part.Close() }()
		got, err := io.ReadAll(part)
		if err != nil || !bytes.Equal(got, raw) || header.Filename != "图像.bin" || header.Header.Get("Content-Type") != "image/png" || r.FormValue("purpose") != "vision" {
			t.Errorf("wrong multipart payload: %x %+v %v", got, header, err)
		}
		_, _ = io.WriteString(w, `{"id":"file-upstream_1"}`)
	}))
	defer up.Close()
	repo := &attachmentsRepository{}
	h := attachmentsHandler(t, store, repo)
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	req.Body = []byte(fmt.Sprintf(`{"model":"m","input":[{"type":"input_file","file_id":%q},{"type":"input_image","file_id":%q}],"metadata":{"count":9007199254740993,"markup":"<unchanged>"},"external":{"file_id":"file-external"}}`, file.ID, file.ID))
	req.ClientHeaders = map[string]string{"Cookie": "client-cookie", "Authorization": "Bearer client-secret"}
	original := bytes.Clone(req.Body)
	target := attachmentsTarget(up.URL + "/nested/v1?version=1")
	prepared, err := h.PrepareFileReferences(context.Background(), req, target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(req.Body, original) {
		t.Fatal("client body mutated")
	}
	if uploads.Load() != 1 || gjson.GetBytes(prepared, "input.0.file_id").Str != "file-upstream_1" || gjson.GetBytes(prepared, "input.1.file_id").Str != "file-upstream_1" {
		t.Fatalf("upload dedup: uploads=%d body=%s", uploads.Load(), prepared)
	}
	if gjson.GetBytes(prepared, "metadata.count").Raw != "9007199254740993" || gjson.GetBytes(prepared, "metadata.markup").Str != "<unchanged>" || gjson.GetBytes(prepared, "external.file_id").Str != "file-external" {
		t.Fatalf("unrelated fields changed: %s", prepared)
	}
	restarted := attachmentsHandler(t, store, repo)
	if _, err := restarted.PrepareFileReferences(context.Background(), req, target); err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 1 {
		t.Fatal("new Handler lost persistent mapping")
	}
	repo.mu.Lock()
	records, _ := json.Marshal(repo.items)
	repo.mu.Unlock()
	if strings.Contains(string(records), target.Secret) || strings.Contains(string(records), target.BaseURL) || strings.Contains(string(records), "client-secret") {
		t.Fatalf("mapping contains secrets: %s", records)
	}
	if err := store.Delete(context.Background(), 11, file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.PrepareFileReferences(context.Background(), req, target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted file reused cache: %v", err)
	}
	if uploads.Load() != 1 {
		t.Fatal("deleted file reached upstream")
	}
}

func TestAttachmentsMappingsIsolateKeyOwnerAndRouting(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("file"))
	var uploads atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := uploads.Add(1)
		_, _ = fmt.Fprintf(w, `{"id":"file-upstream-%d"}`, n)
	}))
	defer up.Close()
	repo := &attachmentsRepository{}
	h := attachmentsHandler(t, store, repo)
	target := attachmentsTarget(up.URL)
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	if _, err := h.PrepareFileReferences(context.Background(), req, target); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*gateway.Request, *gateway.Target){
		func(r *gateway.Request, _ *gateway.Target) { r.Principal.KeyID++ },
		func(_ *gateway.Request, t *gateway.Target) { t.ChannelID++ },
		func(_ *gateway.Request, t *gateway.Target) { t.CredentialID++ },
		func(_ *gateway.Request, t *gateway.Target) { t.Secret += "-rotated" },
		func(_ *gateway.Request, t *gateway.Target) { t.BaseURL += "/new-prefix" },
		func(_ *gateway.Request, t *gateway.Target) {
			t.HeaderOverride = map[string]string{"OpenAI-Project": "different-project"}
		},
	} {
		copyReq, copyTarget := *req, target
		change(&copyReq, &copyTarget)
		before := uploads.Load()
		if _, err := h.PrepareFileReferences(context.Background(), &copyReq, copyTarget); err != nil {
			t.Fatal(err)
		}
		if uploads.Load() != before+1 {
			t.Fatal("routing change reused another mapping")
		}
	}
	foreign := *req
	foreign.Principal.UserID = 22
	before := uploads.Load()
	if _, err := h.PrepareFileReferences(context.Background(), &foreign, target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner resolved file: %v", err)
	}
	if uploads.Load() != before {
		t.Fatal("foreign owner reached upstream")
	}
	foreignFile := filesTestCreate(t, store, 22, []byte("file"))
	foreign.Body = attachmentsRequest(foreignFile.ID, gateway.ProtocolResponses).Body
	if _, err := h.PrepareFileReferences(context.Background(), &foreign, target); err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != before+1 {
		t.Fatal("different owner's file reused mapping")
	}
}

func TestAttachmentsAllOwnersValidatedBeforeUploading(t *testing.T) {
	store := filesTestStore(t)
	own := filesTestCreate(t, store, 11, []byte("own"))
	foreign := filesTestCreate(t, store, 22, []byte("foreign"))
	var called atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		_, _ = io.WriteString(w, `{"id":"file-upstream"}`)
	}))
	defer up.Close()
	repo := &attachmentsRepository{}
	h := attachmentsHandler(t, store, repo)
	req := attachmentsRequest(own.ID, gateway.ProtocolResponses)
	req.Body = []byte(fmt.Sprintf(`{"input":[{"type":"input_file","file_id":%q},{"type":"input_file","file_id":%q}]}`, own.ID, foreign.ID))
	if _, err := h.PrepareFileReferences(context.Background(), req, attachmentsTarget(up.URL)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mixed ownership err=%v", err)
	}
	if called.Load() != 0 || repo.gets != 0 {
		t.Fatal("upload/cache consulted before validating all owners")
	}
}

func TestAttachmentsConcurrentNativeUploadAndProxy(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("native file"))
	var uploads atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "unresolvable.invalid" || r.URL.Path != "/v1/files" {
			t.Errorf("proxy request: %s", r.URL)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		uploads.Add(1)
		time.Sleep(25 * time.Millisecond)
		_, _ = io.WriteString(w, `{"id":"file-proxy"}`)
	}))
	defer proxy.Close()
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	target := attachmentsTarget("http://unresolvable.invalid")
	target.ProxyURL = proxy.URL
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := h.PrepareFileReferences(context.Background(), req, target)
			if err != nil || gjson.GetBytes(body, "input.0.file_id").Str != "file-proxy" {
				t.Errorf("concurrent prepare: %s %v", body, err)
			}
		}()
	}
	wg.Wait()
	if uploads.Load() != 1 {
		t.Fatalf("concurrent upload count=%d", uploads.Load())
	}
}
