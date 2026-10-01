package live

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type filesTestAuth struct{}

func (filesTestAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	switch key {
	case "owner":
		return gateway.Principal{UserID: 11, KeyID: 101}, nil
	case "other":
		return gateway.Principal{UserID: 22, KeyID: 202}, nil
	default:
		return gateway.Principal{}, errors.New("invalid key")
	}
}

func filesTestStore(t *testing.T) *DiskFileStore {
	t.Helper()
	t.Setenv("FILE_STORAGE_RETENTION_DAYS", "30")
	t.Setenv("FILE_STORAGE_USER_LIMIT_MB", "1024")
	t.Setenv("FILE_DELIVERY_TTL_MINUTES", "15")
	store, err := NewDiskFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func filesTestMux(t *testing.T, store FileStore, maxBytes int64, key []byte) *http.ServeMux {
	t.Helper()
	h := &Handler{cfg: Config{Auth: filesTestAuth{}, Files: store, MaxBodyBytes: maxBytes, DeliveryKey: key, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	mux := http.NewServeMux()
	h.registerFiles(mux)
	return mux
}

func filesTestRequest(mux http.Handler, method, path, key string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func filesTestUpload(t *testing.T, mux http.Handler, key string, raw []byte, filename, purpose string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if purpose != "" {
		if err := writer.WriteField("purpose", purpose); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return filesTestRequest(mux, "POST", "/v1/files", key, &body, writer.FormDataContentType())
}

func filesTestCreate(t *testing.T, s *DiskFileStore, owner int64, raw []byte) File {
	t.Helper()
	file, err := s.Create(context.Background(), owner, "test.bin", "user_data", "application/octet-stream", bytes.NewReader(raw), int64(len(raw)+1))
	if err != nil {
		t.Fatal(err)
	}
	return file
}

type filesTestFailReader struct{}

func (filesTestFailReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }
