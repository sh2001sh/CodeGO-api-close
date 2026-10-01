package billing

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBreaker(t *testing.T) {
	now := time.Unix(0, 0)
	b := &breaker{threshold: 3, cooldown: time.Second, now: func() time.Time { return now }}
	b.fail()
	b.fail()
	if b.open() {
		t.Fatal("opened before threshold")
	}
	b.fail()
	if !b.open() {
		t.Fatal("not open after 3 failures")
	}
	now = now.Add(1100 * time.Millisecond)
	if b.open() {
		t.Fatal("still open after cooldown: half-open probe must be allowed")
	}
	b.fail() // probe failed
	if !b.open() {
		t.Fatal("failed probe must reopen immediately")
	}
	b.ok()
	if b.open() {
		t.Fatal("success must close")
	}
}

type fakeRedisErr string

func (e fakeRedisErr) Error() string { return string(e) }
func (fakeRedisErr) RedisError()     {}

func TestIsOutage(t *testing.T) {
	var _ redis.Error = fakeRedisErr("") // the classifier keys on this interface
	live := context.Background()
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"network", live, &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"timeout", live, context.DeadlineExceeded, true},
		{"script bug", live, fakeRedisErr("ERR user_script:1: attempt to compare nil"), false},
		{"loading", live, fakeRedisErr("LOADING Redis is loading the dataset in memory"), true},
		{"failover", live, fakeRedisErr("READONLY You can't write against a read only replica."), true},
		{"client left", gone, context.Canceled, false},
		{"no error", live, nil, false},
	}
	for _, c := range cases {
		if got := isOutage(c.ctx, c.err); got != c.want {
			t.Errorf("%s: isOutage = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestLocalAllowance(t *testing.T) {
	l := newLocalBalances(0.1)
	if err := l.take(1, 1); !errors.Is(err, gateway.ErrBillingUnavailable) {
		t.Fatalf("unknown account admitted: %v", err)
	}
	l.observe(1, 10_000) // allowance 1000
	if err := l.take(1, 600); err != nil {
		t.Fatal(err)
	}
	if err := l.take(1, 500); err == nil {
		t.Fatal("admitted beyond 10% of the last known balance")
	}
	l.adjust(1, -550) // the 600 estimate was really 50
	if err := l.take(1, 900); err != nil {
		t.Fatalf("corrected spend not credited back: %v", err)
	}
	l.resetSpent()
	if err := l.take(1, 1000); err != nil {
		t.Fatalf("allowance not restored after replay: %v", err)
	}
	l.observe(2, -5)
	if err := l.take(2, 1); err == nil {
		t.Fatal("admitted an account whose last known balance was negative")
	}
}

func TestWALGroupCommitAndRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := openWAL(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := w.append(walRecord{Keys: []string{"k"}, Args: []string{strconv.Itoa(i)}}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if w.pending.Load() != 500 {
		t.Fatalf("pending = %d; want 500", w.pending.Load())
	}
	segs, err := w.sealed()
	if err != nil || len(segs) != 1 {
		t.Fatalf("sealed = %v, %v; want the one written segment", segs, err)
	}
	recs, err := readSegment(segs[0].path)
	if err != nil || len(recs) != 500 {
		t.Fatalf("read %d records, %v", len(recs), err)
	}
	if err := w.append(walRecord{Args: []string{"after-rotate"}}); err != nil {
		t.Fatal(err)
	}
	w.close()

	// Simulate a crash mid-write: a torn, unacknowledged tail.
	active := filepath.Join(dir, "wal-000000000002.log")
	f, err := os.OpenFile(active, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"k":["torn`)
	_ = f.Close()

	reopened, err := openWAL(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	if n := reopened.pending.Load(); n != 501 {
		t.Fatalf("pending after restart = %d; want 501 (torn tail ignored)", n)
	}
	segs, _ = reopened.sealed()
	if len(segs) != 2 {
		t.Fatalf("segments after restart = %d; want the 2 written before the crash", len(segs))
	}
}
