package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func TestAttachmentsRealRedisMappingSurvivesRepositoryRestart(t *testing.T) {
	address := os.Getenv("CODEGO_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("CODEGO_TEST_REDIS_ADDR not configured")
	}
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("persistent native file"))
	var uploads atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		uploads.Add(1)
		_, _ = io.WriteString(w, `{"id":"file-redis-persistent"}`)
	}))
	defer up.Close()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer func() { _ = client.Close() }()
	prefix := "test:attachment:" + requestID() + ":"
	repo, err := NewRedisRepository(client, prefix)
	if err != nil {
		t.Fatal(err)
	}
	h := attachmentsHandler(t, store, repo)
	req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
	target := attachmentsTarget(up.URL)
	id := attachmentMappingID(file, req, target)
	defer func() { _ = repo.Delete(context.Background(), id, 11, 101) }()
	if _, err := h.PrepareFileReferences(context.Background(), req, target); err != nil {
		t.Fatal(err)
	}
	secondClient := redis.NewClient(&redis.Options{Addr: address})
	defer func() { _ = secondClient.Close() }()
	restartedRepo, err := NewRedisRepository(secondClient, prefix)
	if err != nil {
		t.Fatal(err)
	}
	restarted := attachmentsHandler(t, store, restartedRepo)
	body, err := restarted.PrepareFileReferences(context.Background(), req, target)
	if err != nil || gjson.GetBytes(body, "input.0.file_id").Str != "file-redis-persistent" || uploads.Load() != 1 {
		t.Fatalf("Redis restart: uploads=%d body=%s err=%v", uploads.Load(), body, err)
	}
	item, err := restartedRepo.Get(context.Background(), id, 11, 101)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(item)
	if bytes.Contains(raw, []byte(target.Secret)) || bytes.Contains(raw, []byte(target.BaseURL)) {
		t.Fatalf("persisted credentials: %s", raw)
	}
	if _, err := restartedRepo.Get(context.Background(), id, 11, 102); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Redis key isolation: %v", err)
	}
}
