package live

import (
	"bytes"
	"context"
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

func TestAttachmentsNativeUploadFailureNoFallbackOrRedirectLeak(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("file"))
	var leaked atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Add(1)
		_, _ = io.WriteString(w, `{"id":"file-sink"}`)
	}))
	defer sink.Close()
	for _, mode := range []string{"http-error", "redirect", "bad-json", "empty-id", "unsafe-id", "large-response"} {
		t.Run(mode, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				switch mode {
				case "http-error":
					w.WriteHeader(500)
					_, _ = io.WriteString(w, "test-upstream-secret")
				case "redirect":
					http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
				case "bad-json":
					_, _ = io.WriteString(w, "bad JSON")
				case "empty-id":
					_, _ = io.WriteString(w, `{"id":""}`)
				case "unsafe-id":
					_, _ = io.WriteString(w, `{"id":"file-../other"}`)
				case "large-response":
					_, _ = io.WriteString(w, strings.Repeat("x", attachmentUploadResponseLimit+1))
				}
			}))
			defer up.Close()
			repo := &attachmentsRepository{}
			h := attachmentsHandler(t, store, repo)
			t.Setenv("FILE_DELIVERY_BASE_URL", "https://delivery.test")
			h.cfg.DeliveryKey = bytes.Repeat([]byte{0x21}, 32)
			req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
			body, err := h.PrepareFileReferences(context.Background(), req, attachmentsTarget(up.URL))
			if err == nil || body != nil {
				t.Fatalf("failed upload fell back: %s %v", body, err)
			}
			if strings.Contains(err.Error(), "test-upstream-secret") {
				t.Fatal("upstream error leaked credentials")
			}
			if len(repo.items) != 0 {
				t.Fatal("failed upload persisted a mapping")
			}
		})
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect destination received upstream credentials")
	}
}

func TestAttachmentsRepositoryFailuresAndCancellation(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("failure test"))
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		_, _ = io.WriteString(w, `{"id":"file-upstream"}`)
	}))
	defer up.Close()
	for _, phase := range []string{"lookup", "persist"} {
		t.Run(phase, func(t *testing.T) {
			repo := &attachmentsRepository{}
			if phase == "lookup" {
				repo.getError = errors.New("repository unavailable")
			} else {
				repo.putError = errors.New("repository unavailable")
			}
			h := attachmentsHandler(t, store, repo)
			before := calls.Load()
			if body, err := h.PrepareFileReferences(context.Background(), attachmentsRequest(file.ID, gateway.ProtocolResponses), attachmentsTarget(up.URL)); err == nil || body != nil {
				t.Fatalf("repository failure continued: %s %v", body, err)
			}
			if phase == "lookup" && calls.Load() != before {
				t.Fatal("lookup failure reached upstream")
			}
			if len(repo.items) != 0 {
				t.Fatal("failed persistence kept a mapping")
			}
		})
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, `{"id":"file-too-late"}`)
	}))
	defer slow.Close()
	repo := &attachmentsRepository{}
	h := attachmentsHandler(t, store, repo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := h.PrepareFileReferences(ctx, attachmentsRequest(file.ID, gateway.ProtocolResponses), attachmentsTarget(slow.URL)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("upload cancellation: %v", err)
	}
}

func TestAttachmentsRejectsCorruptMappingAndInvalidJSON(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("safe bytes"))
	repo := &attachmentsRepository{}
	h := attachmentsHandler(t, store, repo)
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	target := attachmentsTarget("http://never-called.invalid")
	id := attachmentMappingID(file, req, target)
	if err := repo.Put(context.Background(), Locator{ID: id, UserID: 22, KeyID: 101, ChannelID: 7, CredentialID: 9, UpstreamID: "file-foreign"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	repo.items[attachmentsRepositoryKey(id, 11, 101)] = repo.items[attachmentsRepositoryKey(id, 22, 101)]
	repo.mu.Unlock()
	if _, err := h.PrepareFileReferences(context.Background(), req, target); err == nil {
		t.Fatal("foreign persisted mapping accepted")
	}
	for _, body := range []string{fmt.Sprintf(`{"file_id":%q`, file.ID), fmt.Sprintf(`{"type":"input_file","file_id":%q} {}`, file.ID)} {
		req.Body = []byte(body)
		if _, err := h.PrepareFileReferences(context.Background(), req, target); err == nil {
			t.Fatal("invalid file-bearing JSON accepted")
		}
	}
	req = attachmentsRequest(file.ID, gateway.ProtocolResponses)
	req.Principal.KeyID = 0
	if _, err := h.PrepareFileReferences(context.Background(), req, target); err == nil {
		t.Fatal("missing authenticated key accepted")
	}
}
