package billing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

type PostingState uint8

const (
	PostingInProgress PostingState = iota
	PostingCommitted
	PostingAborted
)

type PostingStateLoader interface {
	PostingState(context.Context, string, string) (PostingState, error)
}

// PostingReservationID is stable across callback retries without exposing
// operation text in Redis keys. It is distinct from gateway request IDs.
func PostingReservationID(operationID string) string {
	return fmt.Sprintf("post:%x", sha256.Sum256([]byte(operationID)))
}

// ReservePosting holds a fixed business debit against the same available
// balance and held total as gateway requests. The caller already owns the PG
// account row; all debit holds then remain visible until commit or rollback.
func ReservePosting(ctx context.Context, rdb *redisx.Client, accountID int64, operationID, txID string, amount, balance credits.Micro, version int64, now time.Time) (string, error) {
	if accountID <= 0 || operationID == "" || txID == "" || amount <= 0 {
		return "", fmt.Errorf("billing: posting reservation requires account, operation, transaction and positive amount")
	}
	id := PostingReservationID(operationID)
	k := keysFor(accountID, id)
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := postingReserveScript.Run(callCtx, rdb, []string{k.balance, k.reservation, k.done, redisx.KeyReservationOpen, k.holds, redisx.KeyPostingOpen},
		int64(amount), int64(balance), version, txID, operationID, now.Add(time.Second).UnixMilli(), k.member).Int()
	if err != nil {
		return "", fmt.Errorf("%w: reserve business debit: %v", gateway.ErrBillingUnavailable, err)
	}
	if n == codeInsufficient {
		return "", gateway.ErrInsufficientCredits
	}
	if n != codeReserved {
		return "", fmt.Errorf("%w: business operation is already pending or finalized", gateway.ErrBillingUnavailable)
	}
	return id, nil
}

// PostingKeys supplies the same hold keys to the outbox's atomic delivery.
func PostingKeys(accountID int64, id string) (reservation, done, index, member string) {
	k := keysFor(accountID, id)
	return k.reservation, k.done, k.holds, k.member
}

func postingState(ctx context.Context, rdb *redisx.Client, loader PostingStateLoader, accountID int64, id string) (PostingState, string, error) {
	k := keysFor(accountID, id)
	fields, err := rdb.HMGet(ctx, k.reservation, "business_xid", "operation_id").Result()
	if err != nil {
		return PostingInProgress, "", err
	}
	if fields[0] == nil {
		return PostingAborted, "", nil
	}
	if loader == nil {
		return PostingInProgress, "", fmt.Errorf("billing: business posting state loader missing")
	}
	idValue := fmt.Sprint(fields[0])
	state, err := loader.PostingState(ctx, fmt.Sprint(fields[1]), idValue)
	return state, idValue, err
}

// SweepPostingHolds releases aborted business transactions. Committed entries
// keep their hold until the outbox applies the debit. An in-progress PG
// transaction never releases based on elapsed time alone.
func SweepPostingHolds(ctx context.Context, rdb *redisx.Client, loader PostingStateLoader, now time.Time, limit int64) (int, error) {
	members, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{Key: redisx.KeyPostingOpen, Start: "-inf", Stop: strconv.FormatInt(now.UnixMilli(), 10), ByScore: true, Count: limit}).Result()
	if err != nil {
		return 0, err
	}
	released := 0
	for _, member := range members {
		acct, id, ok := strings.Cut(member, ":")
		accountID, err := strconv.ParseInt(acct, 10, 64)
		if !ok || err != nil {
			return released, fmt.Errorf("billing: malformed posting member %q", member)
		}
		state, txID, err := postingState(ctx, rdb, loader, accountID, id)
		if err != nil {
			return released, err
		}
		k := keysFor(accountID, id)
		if state != PostingAborted {
			next := now.Add(time.Second).UnixMilli()
			if err := rdb.ZAdd(ctx, redisx.KeyPostingOpen, redis.Z{Score: float64(next), Member: member}).Err(); err != nil {
				return released, err
			}
			continue
		}
		n, err := sweepScript.Run(ctx, rdb, []string{k.balance, k.reservation, k.done, redisx.KeyReservationOpen, redisx.StreamBillingEvents, k.holds, redisx.KeyPostingOpen}, now.UnixMilli(), member, id, accountID, txID).Int()
		if err != nil {
			return released, err
		}
		if n == 1 {
			released++
		}
	}
	return released, nil
}
