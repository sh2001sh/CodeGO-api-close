package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func importedBackgroundFixture() (BackgroundJob, []BackgroundEvent) {
	created := time.Date(2025, 7, 10, 12, 0, 0, 123456000, time.UTC)
	job := BackgroundJob{ID: "resp_bg_v2_terminal", UserID: 11, KeyID: 22, Model: "gpt-source", Group: "vip",
		ChannelID: 33, CredentialID: 44, Status: "completed", Billed: true, Native: true, Stream: true,
		UpstreamID: "resp_upstream_source", LastUpstreamSequence: 123, CreatedAt: created, UpdatedAt: created.Add(time.Minute),
		Snapshot: json.RawMessage(`{"id":"resp_bg_v2_terminal","object":"response","model":"gpt-source","status":"completed","output":[{"text":"PRIVATE_IMPORTED_RESULT"}],"error":null}`)}
	events := []BackgroundEvent{
		{Sequence: 0, Type: "response.created", Payload: []byte(`{"type":"response.created","sequence_number":0,"response":{"id":"resp_bg_v2_terminal"}}`)},
		{Sequence: 1, Type: "response.output_text.delta", Payload: []byte(`{"type":"response.output_text.delta","sequence_number":1,"delta":"PRIVATE_IMPORTED_DELTA"}`)},
		{Sequence: 2, Type: "response.completed", Payload: []byte(`{"type":"response.completed","sequence_number":2,"response":{"id":"resp_bg_v2_terminal","status":"completed"}}`)},
	}
	return job, events
}

func TestBackgroundImportInvalidFacts(t *testing.T) {
	job, events := importedBackgroundFixture()
	for _, status := range []string{"queued", "in_progress", "unknown_acceptance", "incomplete"} {
		bad := job
		bad.Status = status
		if _, err := backgroundImportDigest(bad, events); err == nil {
			t.Fatalf("accepted status %s", status)
		}
	}
	for _, change := range []func(*BackgroundJob){
		func(j *BackgroundJob) { j.Billed = false },
		func(j *BackgroundJob) { j.UserID = 0 },
		func(j *BackgroundJob) { j.KeyID = 0 },
		func(j *BackgroundJob) { j.CreatedAt = time.Time{} },
		func(j *BackgroundJob) { j.UpdatedAt = j.CreatedAt.Add(-time.Second) },
		func(j *BackgroundJob) { j.LeaseID = "worker" },
		func(j *BackgroundJob) { j.Body = []byte(`{"secret":"unused"}`) },
		func(j *BackgroundJob) { j.Reservation = json.RawMessage(`{}`) },
		func(j *BackgroundJob) { j.Error = "would_replace_original_error" },
		func(j *BackgroundJob) { j.Snapshot = json.RawMessage(`{"id":"foreign","status":"completed"}`) },
		func(j *BackgroundJob) { j.Snapshot = json.RawMessage(`[]`) },
	} {
		bad := job
		change(&bad)
		if _, err := backgroundImportDigest(bad, events); err == nil {
			t.Fatal("accepted invalid job")
		}
	}
	for _, event := range []BackgroundEvent{
		{Sequence: 1, Type: "response.created", Payload: events[0].Payload},
		{Sequence: 0, Type: "bad\ninjection", Payload: events[0].Payload},
		{Sequence: 0, Type: "response.created", Payload: []byte(`{"type":"response.created","sequence_number":0.0}`)},
		{Sequence: 0, Type: "response.created", Payload: []byte(`{"type":"response.created","sequence_number":0,"response":{"id":"foreign"}}`)},
		{Sequence: 0, Type: "response.created", Payload: []byte(`{"type":"other","sequence_number":0}`)},
	} {
		if _, err := backgroundImportDigest(job, []BackgroundEvent{event}); err == nil {
			t.Fatal("accepted malformed event")
		}
	}
}

func TestRedisBackgroundImportNativeReplayAndIsolation(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	job, events := importedBackgroundFixture()
	if err := repo.VerifyTerminalJob(ctx, job, events); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing read-only asset check: %v", err)
	}
	if err := repo.ImportTerminalJob(ctx, job, events); err != nil {
		t.Fatal(err)
	}
	key, eventKey, pending := repo.keys(job.ID)
	for _, name := range []string{key, eventKey} {
		if ttl := client.PTTL(ctx, name).Val(); ttl != -1 {
			t.Fatalf("source history prematurely expires: %s %v", name, ttl)
		}
	}
	if client.ZCard(ctx, pending).Val() != 0 {
		t.Fatal("imported settled history entered dispatch")
	}
	before := client.HGetAll(ctx, key).Val()
	if err := repo.ImportTerminalJob(ctx, job, events); err != nil {
		t.Fatal(err)
	}
	if before["data"] != client.HGet(ctx, key, "data").Val() {
		t.Fatal("idempotent import rewrote encrypted history")
	}
	got, err := repo.GetOwned(ctx, job.ID, job.UserID, job.KeyID)
	if err != nil || got.Model != job.Model || got.Status != job.Status || got.UpstreamID != job.UpstreamID ||
		got.LastUpstreamSequence != job.LastUpstreamSequence || !got.CreatedAt.Equal(job.CreatedAt) || !got.UpdatedAt.Equal(job.UpdatedAt) || !got.Billed {
		t.Fatalf("native exact source metadata: %+v %v", got, err)
	}
	for _, owner := range [][2]int64{{99, 22}, {11, 99}} {
		if _, err := repo.GetOwned(ctx, job.ID, owner[0], owner[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign owner lookup: %v", err)
		}
		if _, err := repo.Cancel(ctx, job.ID, owner[0], owner[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign owner cancel: %v", err)
		}
	}
	h, _, _, _, billing := backgroundJobsFixture(t, "http://unused.invalid", "openai")
	h.cfg.BackgroundJobs = repo
	principal := gateway.Principal{UserID: job.UserID, KeyID: job.KeyID, Group: "vip"}
	for _, path := range []string{"/v1/responses/" + job.ID, "/v1/responses/" + job.ID + "?stream=true&starting_after=0"} {
		w := httptest.NewRecorder()
		h.serveBackgroundJob(w, httptest.NewRequest("GET", path, nil), principal, job.ID)
		if w.Code != 200 {
			t.Fatalf("native replay HTTP %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(path, "stream=true") {
			if !strings.Contains(w.Body.String(), `"sequence_number":1`) || !strings.Contains(w.Body.String(), `"id":"`+job.ID+`"`) || strings.Contains(w.Body.String(), "event: response.created") {
				t.Fatalf("native SSE resume lost sequence/public ID: %s", w.Body.String())
			}
		} else if gjson.GetBytes(w.Body.Bytes(), "id").Str != job.ID || !bytes.Contains(w.Body.Bytes(), []byte("PRIVATE_IMPORTED_RESULT")) {
			t.Fatalf("native result lost snapshot: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.serveBackgroundJob(w, httptest.NewRequest("POST", "/v1/responses/"+job.ID+"/cancel", nil), principal, job.ID)
	if w.Code != 200 || gjson.GetBytes(w.Body.Bytes(), "status").Str != "completed" {
		t.Fatalf("terminal native cancel: %d %s", w.Code, w.Body.String())
	}
	if err := repo.VerifyTerminalJob(ctx, job, events); err != nil {
		t.Fatalf("terminal cancellation changed settled source facts: %v", err)
	}
	if err := h.Reconcile(ctx, 32); err != nil {
		t.Fatal(err)
	}
	if billing.reserves != 0 || billing.finalizes != 0 {
		t.Fatal("history replay caused a new charge")
	}
	for _, value := range client.HGetAll(ctx, key).Val() {
		if strings.Contains(value, "PRIVATE_IMPORTED_") {
			t.Fatal("unencrypted imported snapshot")
		}
	}
	for _, value := range client.LRange(ctx, eventKey, 0, -1).Val() {
		if strings.Contains(value, "PRIVATE_IMPORTED_") {
			t.Fatal("unencrypted imported event")
		}
	}
}

func TestRedisBackgroundImportConflictCorruptionAndConcurrency(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	job, events := importedBackgroundFixture()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- repo.ImportTerminalJob(ctx, job, events) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("identical concurrent imports: %v", err)
		}
	}
	changed := job
	changed.UserID = 99
	if err := repo.ImportTerminalJob(ctx, changed, events); !errors.Is(err, ErrBackgroundImportConflict) {
		t.Fatalf("changed ownership overwrite: %v", err)
	}
	changed = job
	changed.UpdatedAt = changed.UpdatedAt.Add(time.Second)
	if err := repo.ImportTerminalJob(ctx, changed, events); !errors.Is(err, ErrBackgroundImportConflict) {
		t.Fatalf("changed timestamp overwrite: %v", err)
	}
	key, eventKey, _ := repo.keys(job.ID)
	if err := client.LSet(ctx, eventKey, 0, "corrupt").Err(); err != nil {
		t.Fatal(err)
	}
	if err := repo.ImportTerminalJob(ctx, job, events); err == nil {
		t.Fatal("corrupted event silently repaired")
	}
	if _, err := repo.Events(ctx, job.ID, -1, 10); err == nil {
		t.Fatal("corrupted event replay accepted")
	}
	if err := client.HSet(ctx, key, "data", "corrupt").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetOwned(ctx, job.ID, job.UserID, job.KeyID); err == nil {
		t.Fatal("corrupted source job replay accepted")
	}
	if err := repo.VerifyTerminalJob(ctx, job, events); err == nil {
		t.Fatal("read-only asset check accepted corruption")
	}
}

func TestRedisBackgroundImportNonstreamTerminalHistory(t *testing.T) {
	repo, _ := backgroundRedisTest(t)
	ctx := context.Background()
	for _, status := range []string{"failed", "cancelled"} {
		job, _ := importedBackgroundFixture()
		job.ID, job.Status, job.Stream = "resp_bg_"+status, status, false
		job.Snapshot, _ = json.Marshal(map[string]any{"id": job.ID, "status": status, "error": map[string]any{"message": "original source error"}})
		if err := repo.ImportTerminalJob(ctx, job, nil); err != nil {
			t.Fatal(err)
		}
		if err := repo.VerifyTerminalJob(ctx, job, []BackgroundEvent{}); err != nil {
			t.Fatalf("empty source event normalization: %v", err)
		}
		if events, err := repo.Events(ctx, job.ID, -1, 10); err != nil || len(events) != 0 {
			t.Fatalf("nonstream terminal events: %v %v", events, err)
		}
	}
}

func TestRedisBackgroundImportRejectsExpiryOrDispatchMutation(t *testing.T) {
	for _, fault := range []string{"expiry", "pending"} {
		t.Run(fault, func(t *testing.T) {
			repo, client := backgroundRedisTest(t)
			ctx := context.Background()
			job, events := importedBackgroundFixture()
			if err := repo.ImportTerminalJob(ctx, job, events); err != nil {
				t.Fatal(err)
			}
			key, _, pending := repo.keys(job.ID)
			if fault == "expiry" {
				if err := client.PExpire(ctx, key, time.Hour).Err(); err != nil {
					t.Fatal(err)
				}
			} else if err := client.ZAddArgs(ctx, pending, redis.ZAddArgs{Members: []redis.Z{{Score: 0, Member: job.ID}}}).Err(); err != nil {
				t.Fatal(err)
			}
			if err := repo.VerifyTerminalJob(ctx, job, events); !errors.Is(err, ErrBackgroundImportConflict) {
				t.Fatalf("read-only check missed %s corruption: %v", fault, err)
			}
			if err := repo.ImportTerminalJob(ctx, job, events); !errors.Is(err, ErrBackgroundImportConflict) {
				t.Fatalf("replay silently repaired %s corruption: %v", fault, err)
			}
		})
	}
}
