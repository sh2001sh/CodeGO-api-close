package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Outage mode (plan §5): when Redis is unreachable, a gateway keeps serving
// accounts it has seen recently, up to a fraction of their last known
// balance, and writes every settlement to a local WAL that is replayed into
// Redis once it is back. Accounts this process has never seen are refused.
//
// The allowance is per process: with N gateways an account can overspend by
// up to N × fraction of its balance during an outage. Pick the fraction with
// that in mind.

// ErrBillingDegraded refuses a request during a Redis outage: the account is
// unknown to this gateway or has used up its outage allowance.
var ErrBillingDegraded = fmt.Errorf("%w: redis unavailable and outage allowance exhausted", gateway.ErrBillingUnavailable)

// breaker stops calling Redis for a cooldown after consecutive failures, so
// requests do not each wait out a timeout during an outage.
type breaker struct {
	mu        sync.Mutex
	fails     int
	openUntil time.Time
	threshold int
	cooldown  time.Duration
	now       func() time.Time
}

func (b *breaker) open() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.now().Before(b.openUntil)
}

func (b *breaker) fail() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails++
	if b.fails >= b.threshold {
		b.openUntil = b.now().Add(b.cooldown)
	}
}

func (b *breaker) ok() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails, b.openUntil = 0, time.Time{}
}

// isOutage tells a Redis that cannot be reached from one that answered. A
// script or protocol error is a bug, not an outage, and must not switch the
// gateway into outage mode, except for the replies Redis gives while it is
// failing over or loading.
func isOutage(parent context.Context, err error) bool {
	if err == nil || parent.Err() != nil {
		return false // the caller went away; Redis may be fine
	}
	var rerr redis.Error
	if errors.As(err, &rerr) {
		msg := rerr.Error()
		for _, p := range []string{"LOADING", "READONLY", "MASTERDOWN", "CLUSTERDOWN", "TRYAGAIN"} {
			if strings.HasPrefix(msg, p) {
				return true
			}
		}
		return false
	}
	return true // network error, timeout, closed pool
}

// localBalances remembers the last balance Redis reported for each account
// and what this process has spent on it since Redis went away.
type localBalances struct {
	mu       sync.Mutex
	fraction int64 // fixed once to parts per million; amounts never become floats
	accounts map[int64]*localAccount
}

type localAccount struct {
	balance credits.Micro // last value seen in Redis
	spent   credits.Micro // committed locally since the outage began
}

func newLocalBalances(fraction float64) *localBalances {
	return &localBalances{fraction: int64(math.Round(fraction * 1_000_000)), accounts: make(map[int64]*localAccount)}
}

func (l *localBalances) observe(account int64, balance credits.Micro) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if a := l.accounts[account]; a != nil {
		a.balance = balance
		return
	}
	l.accounts[account] = &localAccount{balance: balance}
}

// take commits amount against the account's outage allowance.
func (l *localBalances) take(account int64, amount credits.Micro) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.accounts[account]
	if a == nil || a.balance <= 0 {
		return ErrBillingDegraded
	}
	num := new(big.Int).Mul(big.NewInt(int64(a.balance)), big.NewInt(l.fraction))
	allowance := credits.Micro(num.Quo(num, big.NewInt(1_000_000)).Int64())
	next, err := a.spent.Add(amount)
	if err != nil || next > allowance {
		return ErrBillingDegraded
	}
	a.spent = next
	return nil
}

// adjust corrects the committed amount once the real charge is known.
func (l *localBalances) adjust(account int64, delta credits.Micro) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if a := l.accounts[account]; a != nil {
		next, err := a.spent.Add(delta)
		if err != nil {
			a.spent = math.MaxInt64
			return
		}
		a.spent = max(0, next)
	}
}

// resetSpent runs once every outage settlement has been replayed into Redis:
// from then on Redis again holds the truth.
func (l *localBalances) resetSpent() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, a := range l.accounts {
		a.spent = 0
	}
}
