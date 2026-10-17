package http

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
)

func TestWalletTransferAdmissionIsBoundedWithoutQueueOrPassword(t *testing.T) {
	set := &walletTransferRequestSet{active: make(map[int]*walletTransferOperation), recent: make(map[walletTransferRequestKey]*walletTransferOperation)}
	req := commerceapp.CreateWalletTransferRequest{RecipientExternalId: "TST002", AmountQuota: 100,
		RequestId: "same-id", PaymentPassword: "must-never-be-retained"}
	var starters atomic.Int32
	var workers sync.WaitGroup
	operations := make(chan *walletTransferOperation, 16)
	for range 16 {
		workers.Go(func() {
			op, start := set.admit(1, req)
			if start {
				starters.Add(1)
			}
			operations <- op
		})
	}
	workers.Wait()
	close(operations)
	operation := set.lookup(1, req.RequestId)
	for op := range operations {
		require.Same(t, operation, op)
	}
	require.EqualValues(t, 1, starters.Load())
	require.NotContains(t, fmt.Sprintf("%#v", operation), req.PaymentPassword)
	for _, change := range []func(*commerceapp.CreateWalletTransferRequest){
		func(req *commerceapp.CreateWalletTransferRequest) { req.RequestId = "another-id" },
		func(req *commerceapp.CreateWalletTransferRequest) { req.RecipientExternalId = "TST003" },
		func(req *commerceapp.CreateWalletTransferRequest) { req.AmountQuota++ },
	} {
		other := req
		change(&other)
		op, start := set.admit(1, other)
		require.Nil(t, op)
		require.False(t, start)
	}
	for sender := 2; sender <= walletTransferMaxInFlight; sender++ {
		op, start := set.admit(sender, req)
		require.NotNil(t, op)
		require.True(t, start)
	}
	op, start := set.admit(99, req)
	require.Nil(t, op)
	require.False(t, start)
	require.Nil(t, set.lookup(99, req.RequestId), "another sender cannot see an operation")

	set.finish(operation, nil, errors.New("commit connection lost"))
	op, start = set.admit(1, req)
	require.Same(t, operation, op)
	require.False(t, start, "unknown commit must not automatically reexecute")
	require.Len(t, set.active, walletTransferMaxInFlight-1)
}

func TestWalletTransferRecentOutcomesAreFiniteAndKnownRejectionMayRetryOriginalSnapshot(t *testing.T) {
	set := &walletTransferRequestSet{active: make(map[int]*walletTransferOperation), recent: make(map[walletTransferRequestKey]*walletTransferOperation)}
	req := commerceapp.CreateWalletTransferRequest{RecipientExternalId: "TST002", AmountQuota: 100, RequestId: "reject"}
	op, start := set.admit(1, req)
	require.True(t, start)
	set.finish(op, nil, commerceschema.ErrWalletTransferInsufficientBalance)
	require.Same(t, op, set.lookup(1, req.RequestId))
	changed := req
	changed.AmountQuota++
	conflict, start := set.admit(1, changed)
	require.Nil(t, conflict)
	require.False(t, start)
	retry, start := set.admit(1, req)
	require.True(t, start)
	require.NotSame(t, op, retry)
	set.finish(retry, nil, errors.New("unknown"))
	for i := range walletTransferRecentLimit + 20 {
		req.RequestId = fmt.Sprintf("bounded-%d", i)
		current, start := set.admit(1, req)
		require.True(t, start)
		set.finish(current, nil, errors.New("unknown"))
		require.LessOrEqual(t, len(set.recent), walletTransferRecentLimit)
	}
	set.expire(time.Now().Add(walletTransferRecentTTL))
	require.Empty(t, set.recent)
}
