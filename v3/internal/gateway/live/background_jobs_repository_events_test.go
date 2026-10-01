package live

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRedisBackgroundEncryptedAtomicEventsAndResume(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	id := "resp_bg_events"
	if err := repo.Create(ctx, backgroundTestJob(id)); err != nil {
		t.Fatal(err)
	}
	job, err := repo.Claim(ctx, id, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	var wg sync.WaitGroup
	type result struct {
		sequence int64
		err      error
	}
	results := make(chan result, count)
	for i := range count {
		wg.Go(func() {
			n, err := repo.Append(ctx, id, "worker", BackgroundEvent{Sequence: -777, Type: "response.output_text.delta", Payload: []byte(fmt.Sprintf("PRIVATE_EVENT_%d", i))})
			results <- result{n, err}
		})
	}
	wg.Wait()
	close(results)
	assigned := make([]int64, 0, count)
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		assigned = append(assigned, result.sequence)
	}
	sort.Slice(assigned, func(i, j int) bool { return assigned[i] < assigned[j] })
	for i, sequence := range assigned {
		if sequence != int64(i) {
			t.Fatalf("noncontiguous atomic sequence: %v", assigned)
		}
	}
	_, eventKey, _ := repo.keys(id)
	stored, err := client.LRange(ctx, eventKey, 0, -1).Result()
	if err != nil || len(stored) != count {
		t.Fatalf("durable events: %d %v", len(stored), err)
	}
	for _, encrypted := range stored {
		if strings.Contains(encrypted, "PRIVATE_EVENT_") || strings.Contains(encrypted, "response.output_text.delta") {
			t.Fatal("event was stored in plaintext")
		}
	}
	all, err := repo.Events(ctx, id, -1, count+1)
	if err != nil || len(all) != count {
		t.Fatalf("all events: %d %v", len(all), err)
	}
	resumed, err := repo.Events(ctx, id, 7, 4)
	if err != nil || len(resumed) != 4 {
		t.Fatalf("resume count: %d %v", len(resumed), err)
	}
	for i, event := range resumed {
		if event.Sequence != int64(8+i) || !bytes.Equal(event.Payload, all[8+i].Payload) || event.Type != "response.output_text.delta" {
			t.Fatalf("resume ordering: %+v", event)
		}
	}
	if events, err := repo.Events(ctx, id, count, 4); err != nil || len(events) != 0 {
		t.Fatalf("cursor after end: %+v %v", events, err)
	}
	if events, err := repo.Events(ctx, id, int64(^uint64(0)>>1), 4); err != nil || len(events) != 0 {
		t.Fatalf("cursor overflow: %+v %v", events, err)
	}
	if _, err := repo.Events(ctx, id, -2, 1); err == nil {
		t.Fatal("accepted invalid cursor")
	}
	if _, err := repo.Events(ctx, "missing", -1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing event owner: %v", err)
	}
	job.Status, job.Billed = "completed", true
	if err := repo.Save(ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Append(ctx, id, "worker", BackgroundEvent{}); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("finalized event append: %v", err)
	}
	if _, err := repo.Events(ctx, id, -1, count); err != nil {
		t.Fatalf("finalized retained events: %v", err)
	}
	if ttl := client.PTTL(ctx, eventKey).Val(); ttl < backgroundHistoryTTL-time.Minute || ttl > backgroundHistoryTTL {
		t.Fatalf("event retention=%v", ttl)
	}
	// Authentication tags are checked even after finalization.
	if err := client.LSet(ctx, eventKey, 0, "bad ciphertext").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Events(ctx, id, -1, 1); err == nil {
		t.Fatal("corrupt event was silently accepted")
	}
}
