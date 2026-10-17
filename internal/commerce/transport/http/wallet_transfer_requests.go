package http

import (
	"errors"
	stdhttp "net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

const (
	walletTransferMaxInFlight = 4
	walletTransferRecentLimit = 128
	walletTransferRecentTTL   = 10 * time.Minute
)

type walletTransferRequestKey struct {
	senderID  int
	requestID string
}

type walletTransferOperation struct {
	senderID    int
	requestID   string
	recipientID string
	amount      int64
	done        chan struct{}
	transfer    *commerceapp.WalletTransferHistoryItem
	err         error
	finishedAt  time.Time
}

type walletTransferRequestSet struct {
	mu     sync.Mutex
	active map[int]*walletTransferOperation
	recent map[walletTransferRequestKey]*walletTransferOperation
}

var walletTransferRequests = &walletTransferRequestSet{
	active: make(map[int]*walletTransferOperation),
	recent: make(map[walletTransferRequestKey]*walletTransferOperation),
}

// admit reserves execution capacity before password verification, with no queue.
// Passwords never enter either map. Identical authenticated requests share one
// operation; conflicting parameters or another sender request are never started.
func (set *walletTransferRequestSet) admit(senderID int, req commerceapp.CreateWalletTransferRequest) (*walletTransferOperation, bool) {
	set.mu.Lock()
	defer set.mu.Unlock()
	set.expire(time.Now())
	key := walletTransferRequestKey{senderID: senderID, requestID: req.RequestId}
	operation := set.active[senderID]
	if operation == nil {
		operation = set.recent[key]
	}
	if operation != nil {
		if operation.requestID != req.RequestId || operation.recipientID != req.RecipientExternalId || operation.amount != req.AmountQuota {
			return nil, false
		}
		select {
		case <-operation.done:
			if walletTransferRejection(operation.err) == nil {
				return operation, false
			}
			// A new explicit POST may retry a known rollback using the same
			// snapshot. Unknown outcomes and successful results never replay here.
			delete(set.recent, key)
		default:
			return operation, false
		}
	}
	if len(set.active) >= walletTransferMaxInFlight {
		return nil, false
	}
	operation = &walletTransferOperation{senderID: senderID, requestID: req.RequestId,
		recipientID: req.RecipientExternalId, amount: req.AmountQuota, done: make(chan struct{})}
	set.active[senderID] = operation
	return operation, true
}

func (set *walletTransferRequestSet) finish(operation *walletTransferOperation, transfer *commerceapp.WalletTransferHistoryItem, err error) {
	set.mu.Lock()
	defer set.mu.Unlock()
	operation.transfer, operation.err, operation.finishedAt = transfer, err, time.Now()
	delete(set.active, operation.senderID)
	set.expire(operation.finishedAt)
	if len(set.recent) >= walletTransferRecentLimit {
		var oldest walletTransferRequestKey
		var at time.Time
		for key, row := range set.recent {
			if at.IsZero() || row.finishedAt.Before(at) {
				oldest, at = key, row.finishedAt
			}
		}
		delete(set.recent, oldest)
	}
	set.recent[walletTransferRequestKey{senderID: operation.senderID, requestID: operation.requestID}] = operation
	close(operation.done)
}

func (set *walletTransferRequestSet) expire(now time.Time) {
	for key, row := range set.recent {
		if now.Sub(row.finishedAt) >= walletTransferRecentTTL {
			delete(set.recent, key)
		}
	}
}

func (set *walletTransferRequestSet) lookup(senderID int, requestID string) *walletTransferOperation {
	set.mu.Lock()
	defer set.mu.Unlock()
	set.expire(time.Now())
	if operation := set.active[senderID]; operation != nil && operation.requestID == requestID {
		return operation
	}
	return set.recent[walletTransferRequestKey{senderID: senderID, requestID: requestID}]
}

func writeWalletTransferPending(c *gin.Context, requestID string, processing bool) {
	status, code := "unconfirmed", "TRANSFER_UNCONFIRMED"
	if processing {
		status, code = "processing", "TRANSFER_PROCESSING"
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(stdhttp.StatusAccepted, gin.H{"success": true, "code": code,
		"data": gin.H{"status": status, "request_id": requestID}})
}

func walletTransferRejection(err error) error {
	for _, rejection := range []error{
		commerceschema.ErrWalletTransferInvalid, commerceschema.ErrWalletTransferRecipientNotFound,
		commerceschema.ErrWalletTransferSelf, commerceschema.ErrWalletTransferPasswordNotSet,
		commerceschema.ErrWalletTransferPasswordIncorrect, commerceschema.ErrWalletTransferPasswordLocked,
		commerceschema.ErrWalletTransferInsufficientBalance, commerceschema.ErrWalletTransferRewardLocked,
	} {
		if errors.Is(err, rejection) {
			return rejection
		}
	}
	return nil
}

// Call only after the done channel closes; its close publishes immutable results.
func writeWalletTransferOutcome(c *gin.Context, operation *walletTransferOperation) {
	c.Header("Cache-Control", "no-store")
	if operation.err == nil && operation.transfer != nil {
		httpapi.ApiSuccess(c, operation.transfer)
		return
	}
	if rejection := walletTransferRejection(operation.err); rejection != nil {
		status, code := stdhttp.StatusOK, "TRANSFER_REJECTED"
		if errors.Is(rejection, commerceschema.ErrWalletTransferPasswordLocked) {
			status, code = stdhttp.StatusTooManyRequests, "PAYMENT_PASSWORD_LOCKED"
		}
		c.JSON(status, gin.H{"success": false, "code": code, "message": rejection.Error(),
			"data": gin.H{"status": "rejected", "request_id": operation.requestID}})
		return
	}
	// Commit/network errors are never automatically replayed, even if the native
	// receipt is not yet visible. Subsequent GETs consult the native receipt first.
	c.JSON(stdhttp.StatusServiceUnavailable, gin.H{"success": false, "code": "TRANSFER_UNCONFIRMED",
		"message": "转账结果暂未确认，请查询原请求",
		"data":    gin.H{"status": "unconfirmed", "request_id": operation.requestID}})
}

func getWalletTransferRequest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	requestID := strings.TrimSpace(c.Param("request_id"))
	transfer, err := commerceapp.LookupWalletTransferReceipt(c.Request.Context(), c.GetInt("id"), requestID)
	if err != nil {
		if errors.Is(err, commerceschema.ErrWalletTransferInvalid) {
			httpapi.ApiError(c, commerceschema.ErrWalletTransferInvalid)
			return
		}
		c.JSON(stdhttp.StatusServiceUnavailable, gin.H{"success": false, "code": "TRANSFER_UNCONFIRMED",
			"message": "转账结果暂未确认，请查询原请求",
			"data":    gin.H{"status": "unconfirmed", "request_id": requestID}})
		return
	}
	if transfer != nil {
		httpapi.ApiSuccess(c, transfer)
		return
	}
	if operation := walletTransferRequests.lookup(c.GetInt("id"), requestID); operation != nil {
		select {
		case <-operation.done:
			if walletTransferRejection(operation.err) != nil {
				writeWalletTransferOutcome(c, operation)
				return
			}
			// No receipt is visible: a cached success never substitutes for DB proof.
			writeWalletTransferPending(c, requestID, false)
		default:
			writeWalletTransferPending(c, requestID, true)
		}
		return
	}
	writeWalletTransferPending(c, requestID, false)
}
