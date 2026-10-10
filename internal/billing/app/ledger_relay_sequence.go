package app

import (
	"context"
	"sync"
)

// Wallets have a unique (user, claude_wallet, quota) account. Queue the short
// relay mutations before borrowing a SQL connection; PostgreSQL still owns
// correctness against other processes and non-relay account writers.
var relayWalletSequence = struct {
	sync.Mutex
	accounts map[int]*relayWalletWait
}{accounts: make(map[int]*relayWalletWait)}

type relayWalletWait struct {
	available  chan struct{}
	references int
}

func waitRelayWallet(ctx context.Context, userID int) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relayWalletSequence.Lock()
	wait := relayWalletSequence.accounts[userID]
	if wait == nil {
		wait = &relayWalletWait{available: make(chan struct{}, 1)}
		wait.available <- struct{}{}
		relayWalletSequence.accounts[userID] = wait
	}
	wait.references++
	relayWalletSequence.Unlock()
	leave := func(acquired bool) {
		relayWalletSequence.Lock()
		if acquired {
			wait.available <- struct{}{}
		}
		wait.references--
		if wait.references == 0 {
			delete(relayWalletSequence.accounts, userID)
		}
		relayWalletSequence.Unlock()
	}
	select {
	case <-ctx.Done():
		leave(false)
		return nil, ctx.Err()
	case <-wait.available:
		// Cancellation and availability can arrive together. Cancellation wins
		// before any caller begins its account lookup or SQL transaction.
		if err := ctx.Err(); err != nil {
			leave(true)
			return nil, err
		}
		return func() { leave(true) }, nil
	}
}

func (f *LedgerRelayFunding) reservationContext() context.Context {
	if f.requestContext != nil {
		return f.requestContext
	}
	return context.Background()
}
