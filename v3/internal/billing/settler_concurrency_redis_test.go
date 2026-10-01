//go:build pgintegration

package billing

import (
	"errors"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Acceptance (tasks.md M2): 1000 concurrent reserve+settle on one account.
func TestConcurrentReserveSettleIsExact(t *testing.T) {
	s, rdb, l, _ := setup(t, 1_000_000)
	const n = 1000
	var wg sync.WaitGroup
	var reserveLat, settleLat [n]time.Duration
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := newReq("c" + strconv.Itoa(i))
			start := time.Now()
			if err := s.Reserve(ctx, req); err != nil {
				errs <- err
				return
			}
			mid := time.Now()
			if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
				errs <- err
				return
			}
			reserveLat[i], settleLat[i] = mid.Sub(start), time.Since(mid)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if b, r := balance(t, rdb); b != 1_000_000-50*n || r != 0 {
		t.Fatalf("balance=%d reserved=%d; want %d/0", b, r, 1_000_000-50*n)
	}
	if got := len(events(t, rdb)); got != n {
		t.Fatalf("stream entries = %d; want %d", got, n)
	}
	if calls := l.calls.Load(); calls != 1 {
		t.Fatalf("1000 cold requests loaded the ledger balance %d times; want 1", calls)
	}
	t.Logf("cold burst of 1000 at once: reserve p50=%v p99=%v | settle p50=%v p99=%v",
		pct(reserveLat[:], 50), pct(reserveLat[:], 99), pct(settleLat[:], 50), pct(settleLat[:], 99))
}

// Warm latency: 64 workers against a loaded account, closer to steady state.
// Measured through Docker Desktop's port forwarding, so it overstates the
// in-datacenter round trip; it is reported, not asserted.
func TestReserveSettleLatencyWarm(t *testing.T) {
	s, _, _, _ := setup(t, 1<<40)
	if err := s.Reserve(ctx, newReq("warmup")); err != nil {
		t.Fatal(err)
	}
	const workers, per = 64, 100
	lat := make([][2]time.Duration, workers*per)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				req := newReq("w" + strconv.Itoa(w*per+i))
				start := time.Now()
				if err := s.Reserve(ctx, req); err != nil {
					t.Error(err)
					return
				}
				mid := time.Now()
				if err := s.Finalize(ctx, req, completed(10, 20)); err != nil {
					t.Error(err)
					return
				}
				lat[w*per+i] = [2]time.Duration{mid.Sub(start), time.Since(mid)}
			}
		}(w)
	}
	wg.Wait()
	res, set := make([]time.Duration, len(lat)), make([]time.Duration, len(lat))
	for i, l := range lat {
		res[i], set[i] = l[0], l[1]
	}
	t.Logf("warm, 64 workers x 100: reserve p50=%v p99=%v | settle p50=%v p99=%v",
		pct(res, 50), pct(res, 99), pct(set, 50), pct(set, 99))
}

// Admission is atomic: 100 concurrent holds of 208 on a balance of 1000
// admit exactly 4.
func TestConcurrentAdmissionNeverOverbooks(t *testing.T) {
	s, rdb, _, _ := setup(t, 1000)
	var ok, short atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch err := s.Reserve(ctx, newReq("a"+strconv.Itoa(i))); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, gateway.ErrInsufficientCredits):
				short.Add(1)
			default:
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if _, r := balance(t, rdb); ok.Load() != 4 || short.Load() != 96 || r != 4*208 {
		t.Fatalf("admitted=%d rejected=%d reserved=%d; want 4/96/832", ok.Load(), short.Load(), r)
	}
}

func pct(d []time.Duration, p int) time.Duration {
	s := slices.Clone(d)
	slices.Sort(s)
	return s[(len(s)-1)*p/100]
}
