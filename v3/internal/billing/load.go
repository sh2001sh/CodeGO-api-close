package billing

import (
	"context"
	"fmt"
	"sync"
)

// installBalance loads the ledger balance once per account among concurrent
// first requests in this process; other processes are fenced by the script.
func (s *Settler) installBalance(ctx context.Context, account int64, k keys) error {
	return s.loads.do(account, func() error {
		// A caller that saw "not loaded" just before another load finished
		// arrives here after that flight ended; skip the redundant ledger read.
		if n, err := s.rdb.Exists(ctx, k.balance).Result(); err == nil && n == 1 {
			return nil
		}
		bal, ver, err := s.loader.LedgerBalance(ctx, account)
		if err != nil {
			return fmt.Errorf("load ledger balance for account %d: %w", account, err)
		}
		return loadScript.Run(ctx, s.rdb, []string{k.balance}, int64(bal), ver).Err()
	})
}

// accountFlight collapses concurrent balance loads of one account.
type accountFlight struct {
	mu    sync.Mutex
	calls map[int64]*loadCall
}

type loadCall struct {
	done chan struct{}
	err  error
}

func (f *accountFlight) do(account int64, fn func() error) error {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = make(map[int64]*loadCall)
	}
	if c, ok := f.calls[account]; ok {
		f.mu.Unlock()
		<-c.done
		return c.err
	}
	c := &loadCall{done: make(chan struct{})}
	f.calls[account] = c
	f.mu.Unlock()
	c.err = fn()
	f.mu.Lock()
	delete(f.calls, account)
	f.mu.Unlock()
	close(c.done)
	return c.err
}
