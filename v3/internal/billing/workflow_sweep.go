package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// AsyncTaskLoader verifies durable task facts. Found tasks always retain their
// hold: terminal-but-unsettled tasks must be finalized by reconciliation.
// Only a verified missing task can be reclaimed after normal hold expiry.
type AsyncTaskLoader interface {
	AsyncTaskExists(context.Context, string, int64, int64) (bool, error)
}

func (s *Settler) retainAsyncHold(ctx context.Context, k keys, now int64) (bool, error) {
	fields, err := s.rdb.HMGet(ctx, k.reservation, "async_request", "async_user", "async_key", "async_kind").Result()
	if err != nil {
		return false, err
	}
	if fields[0] == nil {
		return false, nil
	}
	user, userErr := strconv.ParseInt(fmt.Sprint(fields[1]), 10, 64)
	key, keyErr := strconv.ParseInt(fmt.Sprint(fields[2]), 10, 64)
	if userErr != nil || keyErr != nil || user <= 0 || key <= 0 {
		return true, errors.New("billing: invalid async hold identity")
	}
	var found bool
	if fields[3] == "background" {
		loader, ok := s.loader.(backgroundFactsLoader)
		if !ok {
			return true, errors.New("billing: background facts unavailable; retaining hold")
		}
		found, err = loader.BackgroundTaskExists(ctx, fmt.Sprint(fields[0]), user, key)
	} else {
		loader, ok := s.loader.(AsyncTaskLoader)
		if !ok {
			return true, errors.New("billing: async task loader unavailable; retaining hold")
		}
		found, err = loader.AsyncTaskExists(ctx, fmt.Sprint(fields[0]), user, key)
	}
	if err != nil {
		return true, err
	}
	if !found {
		return false, nil
	}
	return true, s.rdb.ZAdd(ctx, redisx.KeyReservationOpen, redis.Z{Score: float64(now + 60_000), Member: k.member}).Err()
}
