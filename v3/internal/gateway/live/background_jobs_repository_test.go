package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func backgroundRedisTest(t *testing.T) (*RedisBackgroundRepository, *redis.Client) {
	t.Helper()
	address := os.Getenv("CODEGO_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("CODEGO_TEST_REDIS_ADDR is required for real Redis integration tests")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("Redis unavailable: %v", err)
	}
	prefix := fmt.Sprintf("%s:%d", t.Name(), time.Now().UnixNano())
	repo, err := NewRedisBackgroundRepository(client, prefix, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		var cursor uint64
		for {
			keys, next, err := client.Scan(ctx, cursor, repo.prefix+"*", 100).Result()
			if err != nil {
				t.Errorf("scan test namespace: %v", err)
				break
			}
			if len(keys) > 0 {
				if err := client.Del(ctx, keys...).Err(); err != nil {
					t.Errorf("clean test namespace: %v", err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		_ = client.Close()
	})
	return repo, client
}

func backgroundTestJob(id string) BackgroundJob {
	return BackgroundJob{ID: id, UserID: 11, KeyID: 22, ChannelID: 33, CredentialID: 44,
		Status: "queued", Model: "gpt-test", Body: []byte(`{"input":"PRIVATE_BODY_SENTINEL"}`),
		Reservation:    json.RawMessage(`{"hold":"PRIVATE_HOLD_SENTINEL"}`),
		Snapshot:       json.RawMessage(`{"output":"PRIVATE_SNAPSHOT_SENTINEL"}`),
		PricingHeaders: map[string]string{"tag": "PRIVATE_HEADER_SENTINEL"}}
}

func TestRedisBackgroundConstructor(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer func() { _ = client.Close() }()
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := NewRedisBackgroundRepository(client, "", make([]byte, n)); err == nil {
			t.Errorf("accepted key length %d", n)
		}
	}
	if _, err := NewRedisBackgroundRepository(nil, "", make([]byte, 32)); err == nil {
		t.Fatal("accepted missing Redis client")
	}
	a, err := NewRedisBackgroundRepository(client, "unsafe{caller}:a", make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewRedisBackgroundRepository(client, "unsafe{caller}:b", make([]byte, 32))
	if a.prefix == b.prefix || !strings.HasPrefix(a.prefix, "{codego-bg:") || strings.Contains(a.prefix, "caller") {
		t.Fatal("prefix does not isolate namespace and cluster slot")
	}
	job, events, pending := a.keys("x}{untrusted")
	tag := strings.Split(job, "}")[0]
	if strings.Split(events, "}")[0] != tag || strings.Split(pending, "}")[0] != tag || strings.Contains(job, "untrusted") {
		t.Fatal("untrusted ID changed the cluster hash tag")
	}
}

func TestRedisBackgroundEncryptedDurabilityAndOwnership(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	job := backgroundTestJob("resp_bg_secret")
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, job); err == nil {
		t.Fatal("duplicate job replaced durable hold")
	}
	for _, owner := range [][2]int64{{12, 22}, {11, 23}, {0, 22}, {11, 0}} {
		if _, err := repo.GetOwned(ctx, job.ID, owner[0], owner[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetOwned(%v) = %v", owner, err)
		}
		if _, err := repo.Cancel(ctx, job.ID, owner[0], owner[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Cancel(%v) = %v", owner, err)
		}
	}
	key, _, _ := repo.keys(job.ID)
	fields, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range fields {
		if strings.Contains(value, "PRIVATE_") || strings.Contains(value, "gpt-test") {
			t.Fatalf("unencrypted private data in field %s", name)
		}
	}
	if fields["user"] != "11" || fields["key"] != "22" || fields["status"] != "queued" {
		t.Fatal("missing routing/ownership metadata")
	}
	// A new repository process needs only Redis and the same encryption key.
	other := &RedisBackgroundRepository{client: client, prefix: repo.prefix, aead: repo.aead}
	got, err := other.GetOwned(ctx, job.ID, 11, 22)
	if err != nil || !bytes.Equal(got.Body, job.Body) || !bytes.Equal(got.Reservation, job.Reservation) || !bytes.Equal(got.Snapshot, job.Snapshot) || got.PricingHeaders["tag"] != job.PricingHeaders["tag"] {
		t.Fatalf("restart durability: job=%+v err=%v", got, err)
	}
	if got.LeaseID != "" || !got.LeaseUntil.IsZero() || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("invalid initial lease/timestamps")
	}
	if ttl, err := client.PTTL(ctx, key).Result(); err != nil || ttl != -1 {
		t.Fatalf("unfinished hold must not expire: ttl=%v err=%v", ttl, err)
	}
	wrong, _ := NewRedisBackgroundRepository(client, "unused", bytes.Repeat([]byte{8}, 32))
	wrong.prefix = repo.prefix
	if _, err := wrong.GetOwned(ctx, job.ID, 11, 22); err == nil {
		t.Fatal("wrong encryption key was accepted")
	}
	// Flip a bit so the mutation cannot accidentally equal the random byte.
	tampered := []byte(fields["data"])
	tampered[len(tampered)-1] ^= 1
	if err := client.HSet(ctx, key, "data", tampered).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetOwned(ctx, job.ID, 11, 22); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestRedisBackgroundPendingBillingAndHistory(t *testing.T) {
	repo, client := backgroundRedisTest(t)
	ctx := context.Background()
	statuses := []string{"queued", "in_progress", "completed", "failed", "cancelled", "unknown_acceptance"}
	for i, status := range statuses {
		job := backgroundTestJob(fmt.Sprintf("resp_bg_pending_%d", i))
		job.Status = status
		job.CreatedAt = time.Unix(1700000000+int64(i), 0).UTC()
		if err := repo.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := repo.Pending(ctx, 100)
	if err != nil || len(pending) != len(statuses) {
		t.Fatalf("terminal unbilled and ambiguous holds must remain pending: %v %v", pending, err)
	}
	for i, id := range pending {
		if id != fmt.Sprintf("resp_bg_pending_%d", i) {
			t.Fatalf("pending FIFO: %v", pending)
		}
	}
	if first, err := repo.Pending(ctx, 2); err != nil || len(first) != 2 {
		t.Fatalf("pending limit: %v %v", first, err)
	}
	job, err := repo.Claim(ctx, pending[2], "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	job.Billed = true
	if err := repo.Save(ctx, job); err != nil {
		t.Fatal(err)
	}
	left, err := repo.Pending(ctx, 100)
	if err != nil || len(left) != len(statuses)-1 {
		t.Fatalf("finalized removal: %v %v", left, err)
	}
	got, err := repo.GetOwned(ctx, job.ID, 11, 22)
	if err != nil || !got.Billed || got.LeaseID != "" || !got.LeaseUntil.IsZero() {
		t.Fatalf("finalized history: %+v %v", got, err)
	}
	key, _, _ := repo.keys(job.ID)
	if ttl := client.PTTL(ctx, key).Val(); ttl < backgroundHistoryTTL-time.Minute || ttl > backgroundHistoryTTL {
		t.Fatalf("job history retention=%v", ttl)
	}
	if _, err := repo.Claim(ctx, job.ID, "new-worker", time.Minute); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("reclaimed billed job: %v", err)
	}
	job.Billed = false
	if err := repo.Save(ctx, job); !errors.Is(err, ErrBackgroundLeaseConflict) {
		t.Fatalf("unbilled previously billed job: %v", err)
	}
}
