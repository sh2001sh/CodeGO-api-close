package limits

import (
	"sync"
	"testing"
	"time"
)

func TestAuthFailuresWindowAndBound(t *testing.T) {
	now := time.Unix(100, 0)
	a := NewAuthFailures(FailureConfig{MaxFailures: 2, Window: time.Minute, MaxAddresses: 2, Now: func() time.Time { return now }})
	if a.Blocked("one") {
		t.Fatal("unknown address is blocked")
	}
	a.Failed("one")
	if a.Blocked("one") {
		t.Fatal("one failure reached a limit of two")
	}
	a.Failed("one")
	if !a.Blocked("one") || a.Blocked("two") {
		t.Fatal("failures must be isolated by address")
	}
	now = now.Add(time.Minute)
	if a.Blocked("one") {
		t.Fatal("expired failures still block")
	}
	a.Failed("one")
	a.Failed("two")
	a.Failed("three")
	if len(a.entries) != 2 {
		t.Fatalf("unbounded address map: %d", len(a.entries))
	}
}

func TestAuthFailuresConcurrent(t *testing.T) {
	a := NewAuthFailures(FailureConfig{MaxFailures: 20})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { a.Failed("one"); _ = a.Blocked("one") })
	}
	wg.Wait()
	if !a.Blocked("one") {
		t.Fatal("lost concurrent failures")
	}
}
