package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRedisBackgroundLeaseExpiryHeartbeatAndFencing(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	id := "resp_bg_leases"
	if err := repo.Create(ctx, backgroundTestJob(id)); err != nil {
		t.Fatal(err)
	}
	job, err := repo.Claim(ctx, id, "worker-a", time.Second)
	if err != nil || job.LeaseID != "worker-a" || job.LeaseUntil.IsZero() {
		t.Fatalf("claim: %+v %v", job, err)
	}
	if _, err := repo.Claim(ctx, id, "worker-b", time.Minute); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("concurrent lease: %v", err)
	}
	// Longer renewal avoids sleeping and proves the existing lease is refreshed.
	renewed, err := repo.Claim(ctx, id, "worker-a", time.Minute)
	if err != nil || !renewed.LeaseUntil.After(job.LeaseUntil) {
		t.Fatalf("heartbeat: %+v %v", renewed, err)
	}
	stale := job
	stale.LeaseID = "worker-b"
	if err := repo.Save(ctx, stale); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("foreign Save: %v", err)
	}
	if _, err := repo.Append(ctx, id, "worker-b", BackgroundEvent{Type: "wrong"}); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("foreign Append: %v", err)
	}
	// Force the server deadline into the past, without replacing the lease ID.
	key, _, _ := repo.keys(id)
	if err := client.HSet(ctx, key, "lease_until", 1).Err(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, job); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("expired Save: %v", err)
	}
	if _, err := repo.Append(ctx, id, "worker-a", BackgroundEvent{}); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("expired Append: %v", err)
	}
	if _, err := repo.Claim(ctx, id, "worker-b", time.Minute); err != nil {
		t.Fatalf("take over expired lease: %v", err)
	}
	if err := repo.Save(ctx, job); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("stale owner Save after takeover: %v", err)
	}
	if _, err := repo.Claim(ctx, "missing", "worker", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim missing: %v", err)
	}
	if _, err := repo.Claim(ctx, id, "", time.Minute); err == nil {
		t.Fatal("accepted empty lease")
	}
	if _, err := repo.Claim(ctx, id, "worker", time.Nanosecond); err == nil {
		t.Fatal("accepted sub-millisecond lease")
	}
}

func TestRedisBackgroundExclusiveConcurrentClaim(t *testing.T) {
	repo, _ := backgroundRedisTest(t)
	ctx := context.Background()
	if err := repo.Create(ctx, backgroundTestJob("resp_bg_race")); err != nil {
		t.Fatal(err)
	}
	const workers = 24
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := range workers {
		wg.Go(func() {
			_, err := repo.Claim(ctx, "resp_bg_race", fmt.Sprintf("worker-%d", i), time.Minute)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrBackgroundLeaseConflict) {
			t.Errorf("unexpected claim result: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("exclusive claim winners = %d", winners)
	}
}

func TestRedisBackgroundCancellationSurvivesStaleAndConcurrentSave(t *testing.T) {
	repo, _ := backgroundRedisTest(t)
	ctx := context.Background()
	for i := range 12 {
		id := fmt.Sprintf("resp_bg_cancel_%d", i)
		if err := repo.Create(ctx, backgroundTestJob(id)); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.Claim(ctx, id, "worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		stale.Status = "in_progress"
		var wg sync.WaitGroup
		errorsOut := make(chan error, 2)
		wg.Go(func() { errorsOut <- repo.Save(ctx, stale) })
		wg.Go(func() { _, err := repo.Cancel(ctx, id, 11, 22); errorsOut <- err })
		wg.Wait()
		close(errorsOut)
		for err := range errorsOut {
			if err != nil {
				t.Fatal(err)
			}
		}
		// Explicitly save an earlier copy after cancellation has committed.
		if err := repo.Save(ctx, stale); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetOwned(ctx, id, 11, 22)
		if err != nil || !got.CancelRequested || got.Status != "in_progress" {
			t.Fatalf("lost cancellation: %+v %v", got, err)
		}
		if got, err := repo.Cancel(ctx, id, 11, 22); err != nil || !got.CancelRequested {
			t.Fatalf("idempotent cancellation: %+v %v", got, err)
		}
	}
}

func TestRedisBackgroundLeasedJobsCannotStarveNewWork(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		job := backgroundTestJob(fmt.Sprintf("resp_bg_busy_%d", i))
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Claim(ctx, job.ID, "active", 80*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	job := backgroundTestJob("resp_bg_new")
	job.CreatedAt = time.Now().Add(time.Hour) // Client clock skew must not defer queued work.
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.Pending(ctx, 1)
	if err != nil || len(pending) != 1 || pending[0] != job.ID {
		_, _, index := repo.keys(job.ID)
		t.Fatalf("new work starved by active leases: %v %v; index=%v redis_time=%v local_time=%v", pending, err, client.ZRangeWithScores(ctx, index, 0, -1).Val(), client.Time(ctx).Val(), time.Now())
	}
	time.Sleep(100 * time.Millisecond)
	pending, err = repo.Pending(ctx, 100)
	if err != nil || len(pending) != 4 {
		t.Fatalf("expired leases were lost: %v %v", pending, err)
	}
}
