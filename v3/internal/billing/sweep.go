package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// ErrHoldLost is logged when a reservation hash expired before the sweeper
// read it; its held amount stays counted until the balance is reloaded.
var ErrHoldLost = errors.New("billing: reservation hash lost before sweep")

// SweepExpired releases up to limit reservations past their expiry and
// returns how many it released.
func (s *Settler) SweepExpired(ctx context.Context, limit int64) (int, error) {
	now := s.cfg.Now().UnixMilli()
	members, err := s.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key: redisx.KeyReservationOpen, Start: "-inf", Stop: strconv.FormatInt(now, 10), ByScore: true, Count: limit,
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("billing: list expired reservations: %w", err)
	}
	released := 0
	for _, m := range members {
		n, err := s.sweepOne(ctx, m, now)
		if err != nil {
			return released, err
		}
		released += n
	}
	return released, nil
}

func (s *Settler) sweepOne(ctx context.Context, member string, now int64) (int, error) {
	acct, requestID, ok := strings.Cut(member, ":")
	account, err := strconv.ParseInt(acct, 10, 64)
	if !ok || err != nil {
		s.log.Error("billing: malformed open-reservation member; dropping", "member", member)
		return 0, s.rdb.ZRem(ctx, redisx.KeyReservationOpen, member).Err()
	}
	k := keysFor(account, requestID)
	if retained, err := s.retainAsyncHold(ctx, k, now); retained || err != nil {
		return 0, err
	}
	var businessArgs []any
	if strings.HasPrefix(requestID, "post:") {
		loader, _ := s.loader.(PostingStateLoader)
		state, txID, err := postingState(ctx, s.rdb, loader, account, requestID)
		if err != nil {
			return 0, err
		}
		if state != PostingAborted {
			return 0, s.rdb.ZAdd(ctx, redisx.KeyReservationOpen, redis.Z{Score: float64(now + 1000), Member: member}).Err()
		}
		businessArgs = []any{txID}
	}
	args := []any{now, member, requestID, account}
	args = append(args, businessArgs...)
	code, err := sweepScript.Run(ctx, s.rdb,
		[]string{k.balance, k.reservation, k.done, redisx.KeyReservationOpen, redisx.StreamBillingEvents, k.holds, redisx.KeyPostingOpen}, args...).Int()
	if err != nil {
		return 0, fmt.Errorf("billing: sweep %s: %w", member, err)
	}
	switch code {
	case 1:
		s.log.Warn("billing: released expired reservation", "account", account, "request_id", requestID)
		return 1, nil
	case -1:
		s.log.Error("billing: reservation lost before sweep; reserved total may be inflated until reload",
			"account", account, "request_id", requestID, "err", ErrHoldLost)
	}
	return 0, nil
}

// RunSweeper calls SweepExpired every interval until ctx is canceled. Run it
// in the worker, not per gateway; running several is safe but redundant.
func (s *Settler) RunSweeper(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for {
				n, err := s.SweepExpired(ctx, 500)
				if err != nil {
					s.log.Error("billing: sweep failed", "err", err)
					break
				}
				if n < 500 {
					break
				}
			}
		}
	}
}
