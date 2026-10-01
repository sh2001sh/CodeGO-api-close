package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFilesRejectOversizedMIMEBeforePersisting(t *testing.T) {
	store := filesTestStore(t)
	_, err := store.Create(context.Background(), 11, "file.txt", "user_data", "text/plain; charset="+strings.Repeat("a", 2048), strings.NewReader("content"), 1024)
	if !errors.Is(err, ErrInvalidFileUpload) {
		t.Fatalf("oversized MIME accepted: %v", err)
	}
	items, _, err := store.List(context.Background(), 11, 100, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid upload corrupted storage: %v %v", items, err)
	}
}

func TestFilesHTTPBinaryUploadContentAndOwnerIsolation(t *testing.T) {
	store := filesTestStore(t)
	mux := filesTestMux(t, store, 256, bytes.Repeat([]byte{0x37}, 32))
	raw := make([]byte, 256)
	for i := range raw {
		raw[i] = byte(i)
	}
	w := filesTestUpload(t, mux, "owner", raw, "../../binary.bin", "vision")
	if w.Code != 200 {
		t.Fatalf("upload status=%d body=%s", w.Code, w.Body)
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	id := response["id"].(string)
	if !validFileID(id) || response["filename"] != "binary.bin" || response["bytes"] != float64(256) || response["purpose"] != "vision" {
		t.Fatalf("unexpected metadata: %v", response)
	}
	for _, field := range []string{"owner_id", "storage_path", "sha256", "mime_type"} {
		if _, exists := response[field]; exists {
			t.Errorf("private field exposed: %s", field)
		}
	}
	w = filesTestRequest(mux, "GET", "/v1/files/"+id+"/content", "owner", nil, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), raw) {
		t.Fatalf("binary round trip failed: status=%d bytes=%x", w.Code, w.Body.Bytes())
	}
	rangeRequest := httptest.NewRequest("GET", "/v1/files/"+id+"/content", nil)
	rangeRequest.Header.Set("Authorization", "Bearer owner")
	rangeRequest.Header.Set("Range", "bytes=128-130")
	rangeResponse := httptest.NewRecorder()
	mux.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != 206 || !bytes.Equal(rangeResponse.Body.Bytes(), raw[128:131]) {
		t.Fatalf("binary range failed: status=%d bytes=%x", rangeResponse.Code, rangeResponse.Body.Bytes())
	}
	for _, route := range []struct{ method, path string }{{"GET", "/v1/files/" + id}, {"GET", "/v1/files/" + id + "/content"}, {"DELETE", "/v1/files/" + id}} {
		w := filesTestRequest(mux, route.method, route.path, "other", nil, "")
		if w.Code != 404 {
			t.Errorf("other user %s %s status=%d", route.method, route.path, w.Code)
		}
		w = filesTestRequest(mux, route.method, route.path, "", nil, "")
		if w.Code != 401 {
			t.Errorf("anonymous %s %s status=%d", route.method, route.path, w.Code)
		}
	}
	w = filesTestRequest(mux, "GET", "/v1/files", "other", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("foreign list leaked data: %s", w.Body)
	}
	w = filesTestRequest(mux, "DELETE", "/v1/files/"+id, "owner", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":true`) {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	w = filesTestRequest(mux, "GET", "/v1/files/"+id+"/content", "owner", nil, "")
	if w.Code != 404 {
		t.Errorf("deleted file status=%d", w.Code)
	}
}

func TestFilesHTTPRejectsOversizeAndMalformedUploads(t *testing.T) {
	store := filesTestStore(t)
	mux := filesTestMux(t, store, 8, nil)
	if w := filesTestUpload(t, mux, "owner", bytes.Repeat([]byte{1}, 9), "big.bin", ""); w.Code != 413 {
		t.Fatalf("oversize status=%d body=%s", w.Code, w.Body)
	}
	if w := filesTestUpload(t, mux, "owner", nil, "empty.bin", strings.Repeat("x", (1<<20)+1024)); w.Code != 413 {
		t.Fatalf("envelope oversize status=%d body=%s", w.Code, w.Body)
	}
	if w := filesTestRequest(mux, "POST", "/v1/files", "owner", strings.NewReader(`{}`), "application/json"); w.Code != 400 {
		t.Fatalf("JSON upload status=%d", w.Code)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("purpose", "user_data")
	_ = writer.Close()
	if w := filesTestRequest(mux, "POST", "/v1/files", "owner", &body, writer.FormDataContentType()); w.Code != 400 {
		t.Fatalf("missing file status=%d", w.Code)
	}
	for _, count := range []string{"0", "101", "-1", "junk"} {
		if w := filesTestRequest(mux, "GET", "/v1/files?limit="+count, "owner", nil, ""); w.Code != 400 {
			t.Errorf("limit %q status=%d", count, w.Code)
		}
	}
	items, _, err := store.List(context.Background(), 11, 20, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("failed uploads persisted: %+v err=%v", items, err)
	}
	entries, err := os.ReadDir(store.root.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed uploads left files: %+v", entries)
	}
}

func TestFilesStorageConfigurationAndUnavailableStorage(t *testing.T) {
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "bad")
	if _, err := NewDiskFileStore(t.TempDir()); err == nil {
		t.Fatal("invalid retention accepted")
	}
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "0")
	t.Setenv("FILE_STORAGE_USER_LIMIT_MB", "-1")
	if _, err := NewDiskFileStore(t.TempDir()); err == nil {
		t.Fatal("invalid storage limit accepted")
	}
	t.Setenv("FILE_STORAGE_USER_LIMIT_MB", "1024")
	store, err := NewDiskFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	file := filesTestCreate(t, store, 11, []byte("no expiry"))
	if file.ExpiresAt != nil {
		t.Fatal("disabled retention has expiry")
	}
	store.now = func() time.Time { return file.CreatedAt.Add(100 * 365 * 24 * time.Hour) }
	if _, err := store.Get(context.Background(), 11, file.ID); err != nil {
		t.Fatalf("disabled retention file expired: %v", err)
	}
	mux := filesTestMux(t, nil, 8, nil)
	if w := filesTestRequest(mux, "GET", "/v1/files", "owner", nil, ""); w.Code != 503 {
		t.Fatalf("missing storage status=%d", w.Code)
	}
	if w := filesTestRequest(mux, "GET", "/v1/files", "", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous missing storage status=%d", w.Code)
	}
}
